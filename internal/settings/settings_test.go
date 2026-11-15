package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultsAndSave(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "example-karaoke") // does not exist yet
	s, err := Load(dir, "/default/lib")
	if err != nil || !s.FirstRun() || s.Library != "/default/lib" || s.Language != "" || len(s.Feeds) != 0 {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	s.Language = "de"
	s.Feeds = append(s.Feeds, Feed{Address: "https://example.org/feed/secret-token/", Key: "ed25519:abc"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if s.FirstRun() {
		t.Fatal("still first run after Save")
	}
	fi, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v, want 0600", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
	s2, err := Load(dir, "/other/default")
	if err != nil || s2.FirstRun() || s2.Library != "/default/lib" || s2.Language != "de" || len(s2.Feeds) != 1 || s2.Feeds[0].Key != "ed25519:abc" {
		t.Fatalf("reloaded: %+v %v", s2, err)
	}
	// saving again replaces the file
	s2.Library = "/new/lib"
	if err := s2.Save(); err != nil {
		t.Fatal(err)
	}
	if s3, _ := Load(dir, ""); s3.Library != "/new/lib" {
		t.Fatalf("second save: %+v", s3)
	}
}

func TestDamaged(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o600)
	if _, err := Load(dir, "x"); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("damaged file: %v", err)
	}
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{"library": "", "unknown": 1}`), 0o600)
	if s, err := Load(dir, "/d"); err != nil || s.Library != "/d" {
		t.Fatalf("empty library: %+v %v", s, err)
	}
	os.WriteFile(filepath.Join(dir, FileName), make([]byte, 2<<20), 0o600)
	if _, err := Load(dir, "x"); err == nil {
		t.Fatal("2 MB file accepted")
	}
}
