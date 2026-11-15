package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	var out, errs bytes.Buffer
	if run(nil, &out, &errs) != 2 || !strings.Contains(errs.String(), "Usage") {
		t.Fatalf("no command: %q", errs.String())
	}
	if run([]string{"nonsense"}, &out, &errs) != 2 {
		t.Fatal("unknown command")
	}
	errs.Reset()
	if run([]string{"check", "--bundle", filepath.Join(t.TempDir(), "bundle.json")}, &out, &errs) != 1 || errs.Len() == 0 {
		t.Fatal("check of a missing bundle.json")
	}
	key := filepath.Join(t.TempDir(), "app.key")
	if run([]string{"keygen", key}, &out, &errs) != 0 {
		t.Fatal("keygen failed")
	}
	if fi, err := os.Stat(key); err != nil || fi.Size() == 0 {
		t.Fatal("no key file")
	}
	if run([]string{"keygen"}, &out, &errs) != 2 {
		t.Fatal("keygen without a file")
	}
}
