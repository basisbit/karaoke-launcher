package folders

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMark(t *testing.T) {
	dir := t.TempDir()
	if Purpose(dir) != "" {
		t.Fatal("purpose of an empty folder")
	}
	if err := Mark(filepath.Join(dir, "missing"), Songs, "x"); err == nil {
		t.Fatal("missing folder: no error")
	}
	if err := Mark(dir, Songs, "# Songs\n\nText"); err != nil || Purpose(dir) != Songs {
		t.Fatalf("mark: %v %q", err, Purpose(dir))
	}
	if err := Mark(dir, Songs, "# Lieder"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, Name)); !strings.HasPrefix(string(data), "# Lieder\n") {
		t.Fatalf("text not replaced: %q", data)
	}
	if err := Mark(dir, Content("theme"), "# Themes"); !errors.Is(err, ErrOther) || Purpose(dir) != Songs {
		t.Fatalf("second purpose: %v %q", err, Purpose(dir))
	}

	own := t.TempDir()
	os.WriteFile(filepath.Join(own, Name), []byte("# The game's own readme\n"), 0o644)
	if err := Mark(own, Songs, "# Songs"); err != nil || Purpose(own) != "" {
		t.Fatalf("foreign README.md: %v %q", err, Purpose(own))
	}
	if data, _ := os.ReadFile(filepath.Join(own, Name)); string(data) != "# The game's own readme\n" {
		t.Fatalf("foreign README.md changed: %q", data)
	}
}

func TestWithin(t *testing.T) {
	base := t.TempDir()
	a, ab := filepath.Join(base, "a"), filepath.Join(base, "a", "b")
	for _, c := range []struct {
		dir, parent string
		want        bool
	}{
		{a, a, true}, {ab, a, true}, {a, ab, false}, {filepath.Join(base, "ab"), a, false}, {a + string(filepath.Separator), a, true},
	} {
		if got := Within(c.dir, c.parent); got != c.want {
			t.Errorf("Within(%q, %q) = %v", c.dir, c.parent, got)
		}
	}
	if !Overlap(a, ab) || !Overlap(ab, a) || Overlap(a, filepath.Join(base, "c")) {
		t.Error("Overlap")
	}
}

func TestMarkedParent(t *testing.T) {
	base := t.TempDir()
	deep := filepath.Join(base, "lib", "feed", "song")
	os.MkdirAll(deep, 0o755)
	if MarkedParent(deep) != "" {
		t.Fatal("no marker yet")
	}
	Mark(filepath.Join(base, "lib"), Songs, "# Songs")
	if MarkedParent(deep) != Songs || MarkedParent(filepath.Join(base, "lib")) != "" {
		t.Fatal("marker of a parent not found, or the folder itself counted")
	}
}
