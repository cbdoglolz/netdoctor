package main

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed luci-homenet-doctor.lua
var luciController []byte

func installLuCI() error {
	const path = "/usr/lib/lua/luci/controller/homenet_doctor.lua"
	old, _ := os.ReadFile(path)
	if bytes.Equal(old, luciController) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if len(old) > 0 {
		if _, err := os.Stat(path + ".bak"); os.IsNotExist(err) {
			if err = os.WriteFile(path+".bak", old, 0644); err != nil {
				return err
			}
		}
	}
	if err := os.WriteFile(path+".new", luciController, 0644); err != nil {
		return err
	}
	if err := os.Rename(path+".new", path); err != nil {
		return err
	}
	files, _ := filepath.Glob("/tmp/luci-indexcache.*")
	for _, p := range files {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
