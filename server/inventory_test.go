package main

import (
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInventorySoftDeleteRestoreAndDates(t *testing.T) {
	old := engine
	r, err := openEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine = r
	defer func() { engine = old; r.db.Close() }()
	r.discovered = map[string]string{"192.0.2.4": "00:11:22:33:44:55"}
	r.discoveredAt = time.Now()
	stamp := r.discoveredAt.Format(time.RFC3339)
	makeDevices := func() ([]any, map[string]map[string]any) {
		d := map[string]any{"ip": "192.0.2.4", "name": "manual label"}
		return []any{d}, map[string]map[string]any{"192.0.2.4": d}
	}
	ds, by := makeDevices()
	ds = r.inventoryMeta(ds, by, stamp)
	if len(ds) != 1 || by["192.0.2.4"]["lastScannedAt"] != stamp {
		t.Fatal(ds)
	}
	for _, restore := range []bool{false, true} {
		body := `{"ip":"192.0.2.4","restore":false}`
		if restore {
			body = `{"ip":"192.0.2.4","restore":true}`
		}
		w := httptest.NewRecorder()
		inventoryAPI(w, httptest.NewRequest("POST", "/api/inventory", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		ds, by = makeDevices()
		ds = r.inventoryMeta(ds, by, stamp)
		if (len(ds) == 1) != restore {
			t.Fatal("hidden device resurrected", restore, ds)
		}
	}
	if ds[0].(map[string]any)["name"] != "manual label" {
		t.Fatal("remark lost")
	}
}
func TestLayoutBoundsAndPersistence(t *testing.T) {
	if validLayout(map[string]layoutPoint{"x": {math.Inf(1), 0}}) || validLayout(map[string]layoutPoint{"x": {100001, 0}}) {
		t.Fatal("invalid layout accepted")
	}
	old := engine
	r, err := openEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine = r
	defer func() { engine = old; r.db.Close() }()
	w := httptest.NewRecorder()
	layoutAPI(w, httptest.NewRequest("POST", "/api/layout?layer=gateway", strings.NewReader(`{"device":{"x":12,"y":34}}`)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	layoutAPI(w, httptest.NewRequest("GET", "/api/layout?layer=gateway", nil))
	if !strings.Contains(w.Body.String(), `"x":12`) {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	layoutAPI(w, httptest.NewRequest("GET", "/api/layout?layer=physical", nil))
	if strings.Contains(w.Body.String(), "device") {
		t.Fatal("layers mixed")
	}
}
