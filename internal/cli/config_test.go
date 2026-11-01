// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPathKeepsOldLoginUntilNewExists(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("OPENRAIL_CONFIG", "")
	t.Setenv("FIFTY_CONFIG", "")
	write := func(dir string) string {
		p := filepath.Join(base, dir, "config.json")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p, _ := configPath(); p != filepath.Join(base, "openrail", "config.json") {
		t.Errorf("fresh machine: %s", p)
	}
	old := write("5050")
	if p, _ := configPath(); p != old {
		t.Errorf("old login ignored: %s", p)
	}
	newer := write("openrail")
	if p, _ := configPath(); p != newer {
		t.Errorf("new login ignored: %s", p)
	}
}

func TestEnvPrefersOpenrailName(t *testing.T) {
	vars := map[string]string{"FIFTY_URL": "old"}
	a := &App{Getenv: func(k string) string { return vars[k] }}
	if a.env("FIFTY_URL") != "old" {
		t.Error("old name not read")
	}
	vars["OPENRAIL_URL"] = "new"
	if a.env("FIFTY_URL") != "new" {
		t.Error("new name should win")
	}
}
