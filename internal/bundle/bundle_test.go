package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const testKey = "ed25519:aT1kuqmwLsSYNNbLRLUiZKwysGkWiiTenUQFqvy1ZJI"

// fixture is a bundle folder with a valid bundle.json and launcher.json; tests change the maps before write.
type fixture struct {
	dir      string
	bundle   map[string]any
	launcher map[string]any
}

func writeFile(t *testing.T, name string, data []byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(name), 0o755)
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePNG(t *testing.T, name string, w, h int) {
	t.Helper()
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, image.NewNRGBA(image.Rect(0, 0, w, h)))
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(s[:])
}

func newFixture(t *testing.T) *fixture {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "icon.png"), 256, 256)
	writeFile(t, filepath.Join(dir, "assets", "logo.svg"), []byte("<svg/>"))
	writeFile(t, filepath.Join(dir, "docs", "readme.txt"), []byte("hello"))
	writeFile(t, filepath.Join(dir, "docs", "more", "b.txt"), []byte("b"))
	game := []byte("not really a zip")
	writeFile(t, filepath.Join(dir, "game", "game.zip"), game)
	return &fixture{dir: dir,
		bundle: map[string]any{"bundle": 1, "version": "1.0.0", "icon": "icon.png", "summary": "Sing along",
			"description": "A test bundle.", "license": "MIT", "homepage": "https://example.org/",
			"developer": "Test Community", "site": "https://example.org/k/",
			"launcher": map[string]any{"version": "v0.1.0", "repo": "owner/karaoke-launcher"},
			"files":    []any{"docs", "assets/logo.svg"},
			"components": map[string]any{"game": map[string]any{"version": "2.0",
				"windows-amd64": map[string]any{"path": "game/game.zip", "sha256": sum(game), "strip": 0},
				"linux-amd64":   map[string]any{"url": "https://example.org/game-linux.tar.gz", "sha256": sum([]byte("x"))},
			}}},
		launcher: map[string]any{"config": 1, "id": "example", "name": "Example",
			"look":    map[string]any{"logo": "assets/logo.svg"},
			"feeds":   []any{map[string]any{"id": "main", "address": "https://example.org/feed/"}},
			"updates": map[string]any{"url": "https://example.org/k/app.json", "key": testKey},
			"components": map[string]any{"game": map[string]any{"dir": map[string]any{"windows": "{appdir}/game"}},
				"tool": map[string]any{"dir": map[string]any{"windows": "{appdir}/tool"}}}},
	}
}

func (f *fixture) load(t *testing.T) (*Bundle, error) {
	t.Helper()
	for name, v := range map[string]any{"bundle.json": f.bundle, "launcher.json": f.launcher} {
		data, _ := json.Marshal(v)
		writeFile(t, filepath.Join(f.dir, name), data)
	}
	return Load(filepath.Join(f.dir, "bundle.json"))
}

func component(f *fixture, platform string) map[string]any {
	return f.bundle["components"].(map[string]any)["game"].(map[string]any)[platform].(map[string]any)
}

func TestLoad(t *testing.T) {
	f := newFixture(t)
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, s := range b.Shipped {
		rels = append(rels, s.Rel)
		if _, err := os.Stat(s.Abs); err != nil {
			t.Errorf("%s: %v", s.Rel, err)
		}
	}
	if want := []string{"assets/logo.svg", "docs/more/b.txt", "docs/readme.txt", "launcher.json"}; !slices.Equal(rels, want) {
		t.Fatalf("shipped %v, want %v", rels, want)
	}
	g := b.Components["game"]
	if g.Version != "2.0" || g.Files["windows-amd64"].Path != "game/game.zip" || g.Files["linux-amd64"].URL == "" {
		t.Fatalf("component: %+v", g)
	}
}

func TestLoadProblems(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, f *fixture)
		want   string
	}{
		{"bundle version", func(t *testing.T, f *fixture) { f.bundle["bundle"] = 2 }, `"bundle" must be 1`},
		{"unknown field", func(t *testing.T, f *fixture) { f.bundle["summery"] = "x" }, "unknown field"},
		{"version with a slash", func(t *testing.T, f *fixture) { f.bundle["version"] = "1.0/../x" }, `"version"`},
		{"summary missing", func(t *testing.T, f *fixture) { f.bundle["summary"] = " " }, `"summary" is missing`},
		{"summary two lines", func(t *testing.T, f *fixture) { f.bundle["summary"] = "a\nb" }, "one line"},
		{"license missing", func(t *testing.T, f *fixture) { delete(f.bundle, "license") }, `"license" is missing`},
		{"homepage http", func(t *testing.T, f *fixture) { f.bundle["homepage"] = "http://example.org/" }, `"homepage"`},
		{"site http", func(t *testing.T, f *fixture) { f.bundle["site"] = "http://example.org/k/" }, `"site"`},
		{"site without slash", func(t *testing.T, f *fixture) { f.bundle["site"] = "https://example.org/k" }, `"site"`},
		{"updates elsewhere", func(t *testing.T, f *fixture) {
			f.launcher["updates"].(map[string]any)["url"] = "https://example.org/other/app.json"
		}, `"updates.url"`},
		{"launcher release", func(t *testing.T, f *fixture) {
			f.bundle["launcher"] = map[string]any{"version": "latest", "repo": "x"}
		}, `"launcher" needs`},
		{"icon absolute", func(t *testing.T, f *fixture) { f.bundle["icon"] = filepath.Join(f.dir, "icon.png") }, `"icon"`},
		{"icon outside", func(t *testing.T, f *fixture) { f.bundle["icon"] = "../icon.png" }, `"icon"`},
		{"icon not png", func(t *testing.T, f *fixture) { f.bundle["icon"] = "assets/logo.svg" }, "not a PNG"},
		{"icon small", func(t *testing.T, f *fixture) { writePNG(t, filepath.Join(f.dir, "icon.png"), 128, 128) }, "at least 256"},
		{"icon not square", func(t *testing.T, f *fixture) { writePNG(t, filepath.Join(f.dir, "icon.png"), 300, 256) }, "square"},
		{"files outside", func(t *testing.T, f *fixture) { f.bundle["files"] = []any{"../outside"} }, `"files"`},
		{"files backslash", func(t *testing.T, f *fixture) { f.bundle["files"] = []any{`docs\readme.txt`} }, `"files"`},
		{"files missing", func(t *testing.T, f *fixture) { f.bundle["files"] = []any{"nothing"} }, `"files"`},
		{"files reserved", func(t *testing.T, f *fixture) {
			writeFile(t, filepath.Join(f.dir, ".COMPONENT-game.json"), nil)
			f.bundle["files"] = []any{".COMPONENT-game.json"}
		}, "keeps for itself"},
		{"files launcher name", func(t *testing.T, f *fixture) {
			writeFile(t, filepath.Join(f.dir, "karaoke-launcher.exe"), nil)
			f.bundle["files"] = []any{"karaoke-launcher.exe"}
		}, "keeps for itself"},
		{"look outside", func(t *testing.T, f *fixture) { f.launcher["look"] = map[string]any{"logo": "../logo.svg"} }, "launcher.json"},
		{"look missing file", func(t *testing.T, f *fixture) { f.launcher["look"] = map[string]any{"background": "assets/bg.png"} }, "look.background"},
		{"launcher.json broken", func(t *testing.T, f *fixture) { f.launcher["config"] = 2 }, `"config" must be 1`},
		{"component unknown", func(t *testing.T, f *fixture) {
			f.bundle["components"].(map[string]any)["other"] = map[string]any{"version": "1"}
		}, `"other" is not in "components"`},
		{"component launcher", func(t *testing.T, f *fixture) {
			f.bundle["components"].(map[string]any)["launcher"] = map[string]any{"version": "1"}
		}, `"launcher" is not in "components"`},
		{"component without files", func(t *testing.T, f *fixture) {
			f.bundle["components"].(map[string]any)["tool"] = map[string]any{"version": "1"}
		}, "no archive"},
		{"component version", func(t *testing.T, f *fixture) {
			f.bundle["components"].(map[string]any)["game"].(map[string]any)["version"] = ""
		}, `"version"`},
		{"platform name", func(t *testing.T, f *fixture) {
			f.bundle["components"].(map[string]any)["game"].(map[string]any)["Windows"] = component(f, "linux-amd64")
		}, `like "windows-amd64"`},
		{"unknown file field", func(t *testing.T, f *fixture) { component(f, "linux-amd64")["sha"] = "x" }, "unknown field"},
		{"url and path", func(t *testing.T, f *fixture) { component(f, "linux-amd64")["path"] = "game/game.zip" }, `either "url" or "path"`},
		{"no url and no path", func(t *testing.T, f *fixture) { delete(component(f, "linux-amd64"), "url") }, `either "url" or "path"`},
		{"url http", func(t *testing.T, f *fixture) { component(f, "linux-amd64")["url"] = "http://example.org/g.zip" }, "linux-amd64"},
		{"url not an archive", func(t *testing.T, f *fixture) { component(f, "linux-amd64")["url"] = "https://example.org/g.exe" }, "not a .zip"},
		{"path outside", func(t *testing.T, f *fixture) { component(f, "windows-amd64")["path"] = "../game.zip" }, `"path"`},
		{"path missing", func(t *testing.T, f *fixture) { component(f, "windows-amd64")["path"] = "game/missing.zip" }, "windows-amd64"},
		{"hash format", func(t *testing.T, f *fixture) { component(f, "windows-amd64")["sha256"] = "abc" }, `"sha256"`},
		{"hash wrong", func(t *testing.T, f *fixture) { component(f, "windows-amd64")["sha256"] = sum([]byte("other")) }, "but bundle.json says"},
		{"strip negative", func(t *testing.T, f *fixture) { component(f, "windows-amd64")["strip"] = -1 }, `"strip"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			c.change(t, f)
			_, err := f.load(t)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want a problem with %q", err, c.want)
			}
		})
	}
}

func TestLoadAllProblems(t *testing.T) {
	f := newFixture(t)
	f.bundle["summary"] = ""
	f.bundle["site"] = "ftp://x/"
	_, err := f.load(t)
	var p Problems
	if !errors.As(err, &p) || len(p) != 2 {
		t.Fatalf("want both problems at once: %v", err)
	}
	f = newFixture(t)
	f.launcher["config"] = 2
	if _, err := f.load(t); !errors.As(err, &p) || len(p) != 1 {
		t.Fatalf("a broken launcher.json must not bring follow-up problems: %v", err)
	}
}

func TestLoadLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need extra rights on Windows")
	}
	f := newFixture(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), []byte("x"))
	if err := os.Symlink(outside, filepath.Join(f.dir, "docs", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.load(t); err == nil || !strings.Contains(err.Error(), "is a link") {
		t.Fatalf("link shipped: %v", err)
	}
}

// TestExample loads examples/bundle with stand-in archives (make-game.sh builds the real ones).
func TestExample(t *testing.T) {
	src := filepath.Join("..", "..", "examples", "bundle")
	dir := t.TempDir()
	err := filepath.WalkDir(src, func(name string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || strings.HasSuffix(name, ".zip") {
			return err
		}
		rel, _ := filepath.Rel(src, name)
		data, err := os.ReadFile(name)
		writeFile(t, filepath.Join(dir, rel), data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var b map[string]any
	data, _ := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	for k, v := range b["components"].(map[string]any)["game"].(map[string]any) {
		if entry, ok := v.(map[string]any); ok {
			body := []byte("stand-in " + k)
			writeFile(t, filepath.Join(dir, filepath.FromSlash(entry["path"].(string))), body)
			entry["sha256"] = sum(body)
		}
	}
	data, _ = json.Marshal(b)
	writeFile(t, filepath.Join(dir, "bundle.json"), data)
	if _, err := Load(filepath.Join(dir, "bundle.json")); err != nil {
		t.Fatal(err)
	}
}

func TestLauncherBinary(t *testing.T) {
	f := newFixture(t)
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "karaoke-launcher-windows-amd64.exe"), []byte("exe"))
	if p, err := b.LauncherBinary(ctx, "windows-amd64", local, ""); err != nil || filepath.Base(p) != "karaoke-launcher-windows-amd64.exe" {
		t.Fatalf("from folder: %q %v", p, err)
	}
	if _, err := b.LauncherBinary(ctx, "linux-amd64", local, ""); err == nil {
		t.Fatal("missing binary in folder accepted")
	}

	exe, linux := []byte("windows launcher"), []byte("linux launcher")
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/owner/karaoke-launcher/releases/download/v0.1.0/SHA256SUMS":
			w.Write([]byte(strings.TrimPrefix(sum(exe), "sha256:") + "  karaoke-launcher-windows-amd64.exe\n" +
				strings.TrimPrefix(sum([]byte("other")), "sha256:") + "  karaoke-launcher-linux-amd64\n"))
		case "/owner/karaoke-launcher/releases/download/v0.1.0/karaoke-launcher-windows-amd64.exe":
			w.Write(exe)
		case "/owner/karaoke-launcher/releases/download/v0.1.0/karaoke-launcher-linux-amd64":
			w.Write(linux)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer func(s string) { releaseBase = s }(releaseBase)
	releaseBase = srv.URL
	cache := t.TempDir()
	p, err := b.LauncherBinary(ctx, "windows-amd64", "", cache)
	if data, _ := os.ReadFile(p); err != nil || string(data) != string(exe) {
		t.Fatalf("download: %q %v", data, err)
	}
	before := requests
	if _, err := b.LauncherBinary(ctx, "windows-amd64", "", cache); err != nil || requests != before+1 {
		t.Fatalf("second call: %v, %d requests (want only SHA256SUMS)", err, requests-before)
	}
	if _, err := b.LauncherBinary(ctx, "linux-amd64", "", cache); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("wrong hash accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "launcher-v0.1.0", "karaoke-launcher-linux-amd64")); err == nil {
		t.Fatal("a file with the wrong hash was kept")
	}
	if _, err := b.LauncherBinary(ctx, "darwin-arm64", "", cache); err == nil || !strings.Contains(err.Error(), "does not list") {
		t.Fatalf("missing entry: %v", err)
	}
}
