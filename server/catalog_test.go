package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupCatalogTest(t *testing.T) {
	t.Helper()
	catalog.Lock()
	cfg, doc, dir, saved, contact, err, acks := catalog.settings, catalog.doc, catalog.dir, catalog.savedAt, catalog.contactAt, catalog.err, catalog.acks
	catalog.settings = catalogSettings{Enabled: true, ManagerID: strings.Repeat("a", 32), ManagerURL: "http://192.168.2.6:8088", Token: strings.Repeat("d", 64)}
	catalog.doc = catalogDocument{}
	catalog.dir = t.TempDir()
	catalog.acks = nil
	catalog.Unlock()
	peers.Lock()
	oldID := peers.config.ID
	peers.config.ID = strings.Repeat("b", 32)
	peers.Unlock()
	t.Cleanup(func() {
		catalog.Lock()
		catalog.settings, catalog.doc, catalog.dir, catalog.savedAt, catalog.contactAt, catalog.err, catalog.acks = cfg, doc, dir, saved, contact, err, acks
		catalog.Unlock()
		peers.Lock()
		peers.config.ID = oldID
		peers.Unlock()
	})
}
func catalogTestDocument(rev uint64) catalogDocument {
	c := catalogContent{Devices: []catalogDevice{{peerAnnotation: peerAnnotation{IP: "192.168.2.136", Name: "共享手机", Kind: "phone", Icon: "phone"}, MAC: "02:11:22:33:44:55"}}, Layouts: map[string]map[string]layoutPoint{"gateway": {"device:192.168.2.6": {X: 50, Y: 100}}}}
	return catalogDocument{Schema: 1, ManagerID: strings.Repeat("a", 32), Revision: rev, Hash: catalogHash(c), UpdatedAt: time.Now(), Content: c}
}
func sendCatalogTest(d *catalogDocument, token string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(catalogWire{Document: d})
	r := httptest.NewRequest("POST", "https://node/catalog", bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-HND-Manager", strings.Repeat("a", 32))
	w := httptest.NewRecorder()
	catalogHandler(w, r)
	return w
}
func TestCatalogPersistenceSurvivesManagerLossAndRestart(t *testing.T) {
	setupCatalogTest(t)
	d := catalogTestDocument(1)
	if w := sendCatalogTest(&d, strings.Repeat("d", 64)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before, e := os.ReadFile(filepath.Join(catalog.dir, "catalog.json"))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(catalog.settings)
	os.WriteFile(filepath.Join(catalog.dir, "catalog-settings.json"), b, 0600)
	if e = initCatalog(catalog.dir); e != nil {
		t.Fatal(e)
	}
	if catalog.doc.Hash != d.Hash || catalog.doc.Revision != 1 {
		t.Fatal("replica did not survive restart")
	}
	// There is no live peer payload. Display must still use the durable document.
	s := map[string]any{"devices": []any{map[string]any{"ip": "192.168.2.136", "mac": "02:11:22:33:44:55", "name": "old"}}}
	overlayCatalogSnapshot(s)
	if s["devices"].([]any)[0].(map[string]any)["name"] != "共享手机" {
		t.Fatal("offline display lost")
	}
	after, _ := os.ReadFile(filepath.Join(catalog.dir, "catalog.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("restart rewrote content")
	}
}
func TestCatalogRejectsRollbackConflictAndTampering(t *testing.T) {
	setupCatalogTest(t)
	d := catalogTestDocument(2)
	if sendCatalogTest(&d, strings.Repeat("d", 64)).Code != 200 {
		t.Fatal("initial save")
	}
	old := catalogTestDocument(1)
	if sendCatalogTest(&old, strings.Repeat("d", 64)).Code != 409 {
		t.Fatal("rollback accepted")
	}
	same := d
	same.Content = catalogContent{Devices: []catalogDevice{}, Layouts: map[string]map[string]layoutPoint{}}
	same.Hash = catalogHash(same.Content)
	if sendCatalogTest(&same, strings.Repeat("d", 64)).Code != 409 {
		t.Fatal("same version conflict accepted")
	}
	tampered := d
	tampered.Hash = strings.Repeat("0", 64)
	if sendCatalogTest(&tampered, strings.Repeat("d", 64)).Code != 400 {
		t.Fatal("tampering accepted")
	}
	if catalog.doc.Hash != d.Hash {
		t.Fatal("valid replica replaced")
	}
}
func TestCatalogNoAcknowledgementOnPersistenceFailure(t *testing.T) {
	setupCatalogTest(t)
	catalog.dir = filepath.Join(catalog.dir, "missing")
	d := catalogTestDocument(1)
	if sendCatalogTest(&d, strings.Repeat("d", 64)).Code != 500 || catalog.doc.Revision != 0 {
		t.Fatal("failed backup acknowledged")
	}
}
func TestCatalogTokenDoesNotExpandReadonlyOrControlAccess(t *testing.T) {
	setupCatalogTest(t)
	d := catalogTestDocument(1)
	if sendCatalogTest(&d, strings.Repeat("a", 64)).Code != 401 {
		t.Fatal("readonly token accepted")
	}
	catalog.settings.ManagerID = localNodeID()
	d.ManagerID = localNodeID()
	if sendCatalogTest(&d, strings.Repeat("d", 64)).Code != 401 {
		t.Fatal("manager accepted replica overwrite")
	}
}
func TestCatalogCentralOwnershipAndNodeLocalFlags(t *testing.T) {
	setupCatalogTest(t)
	catalog.doc = catalogTestDocument(1)
	c := runtimeConfig{Devices: []deviceConfig{{IP: "192.168.2.136", Name: "本机旧名称", Kind: "phone", Gateway: "192.168.2.144", MonitorDisabled: true}}}
	overlayCatalogConfig(&c)
	d := c.Devices[0]
	if d.Name != "共享手机" || !d.MonitorDisabled || d.Gateway != "192.168.2.144" {
		t.Fatal("node network or flags overwritten", d)
	}
	if catalogDeviceWriteAllowed(d) != nil {
		t.Fatal("local flag save denied")
	}
	d.Name = "other"
	if catalogDeviceWriteAllowed(d) == nil {
		t.Fatal("replica metadata write accepted")
	}
	s := map[string]any{"devices": []any{map[string]any{"ip": "192.168.2.136", "mac": "02:11:22:33:44:99", "name": "new device"}}}
	overlayCatalogSnapshot(s)
	if s["devices"].([]any)[0].(map[string]any)["name"] != "new device" {
		t.Fatal("IP reuse mislabelled")
	}
}
func TestCatalogLayoutIdentityDistinguishesLocalDoctor(t *testing.T) {
	s := map[string]any{"devices": []any{map[string]any{"id": "doctor", "ip": "192.168.2.144"}, map[string]any{"id": "remote6", "ip": "192.168.2.6"}}}
	if catalogLayoutKey("doctor", s) != "device:192.168.2.144" || catalogLayoutKey("remote6", s) != "device:192.168.2.6" {
		t.Fatal("doctor alias confused nodes")
	}
}
func TestCatalogKeepsPreviousVersionAndDuplicateDeliveryIsIdempotent(t *testing.T) {
	setupCatalogTest(t)
	d := catalogTestDocument(1)
	sendCatalogTest(&d, strings.Repeat("d", 64))
	saved := catalog.savedAt
	if sendCatalogTest(&d, strings.Repeat("d", 64)).Code != 200 || catalog.savedAt != saved {
		t.Fatal("identical delivery rewrote disk")
	}
	d.Revision = 2
	d.Content.Devices[0].Name = "updated"
	d.Hash = catalogHash(d.Content)
	if sendCatalogTest(&d, strings.Repeat("d", 64)).Code != 200 {
		t.Fatal("update failed")
	}
	b, e := os.ReadFile(filepath.Join(catalog.dir, "catalog-backup-01.json"))
	if e != nil {
		t.Fatal(e)
	}
	var old catalogDocument
	json.Unmarshal(b, &old)
	if old.Revision != 1 || old.Content.Devices[0].Name != "共享手机" {
		t.Fatal("old version not retained")
	}
}
