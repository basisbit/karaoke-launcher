// Package testfeed builds small feeds for tests: dummy songs and a theme pack, signed with a key made on the spot.
// Only tests use it.
package testfeed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	kfeed "github.com/basisbit/karaoke-feeds/tool"
)

const songMeta = `{"licenses": {"*": {"spdx": "CC-BY-4.0", "attribution": "Test Artist", "source": "https://example.org/src"}}}`

// Feed is a built feed: Dir is the source folder (change it and call Build again), Pub the published folder.
type Feed struct {
	t        testing.TB
	Dir, Pub string
	KeyFile  string
	Key      string // public key, "ed25519:…"
}

// Signed builds a signed feed with the song pack "pop" (songs a, b) and the theme pack "themes" (item blue-theme:
// Blue.ini and Blue/bg.png).
func Signed(t testing.TB) *Feed {
	t.Helper()
	f := &Feed{t: t, Dir: t.TempDir()}
	f.KeyFile = filepath.Join(t.TempDir(), "feed.key")
	quiet(func() {
		if kfeed.Run([]string{"kfeed", "keygen", f.KeyFile}, "test") != 0 {
			t.Fatal("keygen failed")
		}
	})
	_, pub, err := kfeed.Sign(f.KeyFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.Key = pub
	os.WriteFile(filepath.Join(f.Dir, "feed-config.json"), []byte(`{"name":"Test Feed","operator":{"name":"Tester","contact":"mailto:t@example.com"},
		"keys": ["`+pub+`"], "extras": "extras",
		"catalogs": {"Pop": {"id": "pop"}, "Themes": {"id": "themes", "kind": "theme", "names": {"en": "Themes", "de": "Designs"}}}}`), 0o644)
	for _, n := range []string{"a", "b"} {
		d := filepath.Join(f.Dir, "songs", "Pop", n)
		os.MkdirAll(d, 0o755)
		txt := "#ARTIST:Tester\n#TITLE:" + n + "\n#BPM:300\n#GAP:0\n#MP3:" + n + ".mp3\n: 0 4 60 la\n: 8 4 62 la\nE\n"
		os.WriteFile(filepath.Join(d, n+".txt"), []byte(txt), 0o644)
		os.WriteFile(filepath.Join(d, n+".mp3"), []byte(strings.Repeat("dummy "+n, 500)), 0o644)
		os.WriteFile(filepath.Join(d, "feed-meta.json"), []byte(songMeta), 0o644)
	}
	f.SetThemeFile("Blue.ini", "[Theme]\nName=Blue\n")
	f.SetThemeFile("Blue/bg.png", "dummy png")
	os.WriteFile(filepath.Join(f.Dir, "extras", "Themes", "Blue", "feed-meta.json"),
		[]byte(`{"id": "blue-theme", "names": {"en": "Blue", "de": "Blau"}, "license": {"spdx": "CC0-1.0"}}`), 0o644)
	f.Build()
	return f
}

// SetThemeFile writes a file of the item blue-theme; call Build afterwards.
func (f *Feed) SetThemeFile(path, content string) {
	p := filepath.Join(f.Dir, "extras", "Themes", "Blue", filepath.FromSlash(path))
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(content), 0o644)
}

// Build (re)builds the published folder.
func (f *Feed) Build() {
	f.t.Helper()
	var code int
	quiet(func() { code = kfeed.Run([]string{"kfeed", "build", f.Dir, "--key", f.KeyFile}, "test") })
	if code != 0 {
		f.t.Fatalf("kfeed build: exit %d", code)
	}
	f.Pub = filepath.Join(f.Dir, "public")
}

// quiet runs fn with standard output discarded (kfeed prints reports).
func quiet(fn func()) {
	old := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	defer func() { os.Stdout.Close(); os.Stdout = old }()
	fn()
}
