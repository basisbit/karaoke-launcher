package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// usdxFixture is a bundle with "game": "ultrastar-deluxe" and a Flatpak with the community's own permission. The
// Windows archive is replaced by a local one (the community's value wins), so that no test downloads the game.
func usdxFixture(t *testing.T) (*fixture, Options) {
	f, o := windowsFixture(t)
	f.bundle["game"] = "ultrastar-deluxe"
	game := f.bundle["components"].(map[string]any)["game"].(map[string]any)
	delete(game, "linux-amd64")
	f.bundle["components"] = map[string]any{"ultrastar-deluxe": map[string]any{"windows-amd64": game["windows-amd64"]}}
	delete(f.launcher, "components")
	f.launcher["app_id"] = "org.example.Karaoke"
	f.launcher["start"] = map[string]any{"linux": []any{"/app/bin/ultrastardx", "-SongPath", "{library}", "-Fullscreen"}}
	f.bundle["flatpak"] = map[string]any{"runtime": "org.freedesktop.Platform", "runtime_version": "26.08",
		"sdk": "org.freedesktop.Sdk", "finish_args": []any{"--socket=wayland", "--share=network"}}
	writeFile(t, filepath.Join(o.LauncherDir, "karaoke-launcher-linux-amd64"), []byte("launcher program"))
	return f, o
}

func TestGame(t *testing.T) {
	f, o := usdxFixture(t)
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	if b.Game == nil || b.Game.ID != "ultrastar-deluxe" || !strings.Contains(b.Game.Source, "v"+b.Game.Version) {
		t.Fatalf("game: %+v", b.Game)
	}
	// bundle.json: the game's version, the community's archive
	c := b.Components["ultrastar-deluxe"]
	if c.Version != b.Game.Version || c.Files["windows-amd64"].Path != "game/game.zip" {
		t.Fatalf("component: %+v", c)
	}
	if b.Mac == nil || len(b.Mac.Apps) != 1 || b.Mac.Apps[0].App != "UltraStar Deluxe.app" {
		t.Fatalf("macos: %+v", b.Mac)
	}
	fp := b.Flatpak
	if !slices.Contains(fp.FinishArgs, "--device=all") || !slices.Contains(fp.FinishArgs, "--share=network") ||
		slices.Index(fp.FinishArgs, "--device=all") > slices.Index(fp.FinishArgs, "--share=network") {
		t.Fatalf("finish_args are not joined (the game's first): %v", fp.FinishArgs)
	}
	if n := len(slices.DeleteFunc(slices.Clone(fp.FinishArgs), func(a string) bool { return a != "--socket=wayland" })); n != 1 {
		t.Fatalf("--socket=wayland %d times", n)
	}
	if len(fp.Modules) != 0 || len(fp.SdkExtensions) != 1 || fp.BuildOptions["append-path"] == nil || len(fp.Cleanup) == 0 {
		t.Fatalf("flatpak: %+v", fp)
	}
	// launcher.json: the game's start command and folders; the community's start for Linux wins
	cfg := b.Config
	if cfg.Start["windows"][0] != "game/ultrastardx.exe" || !slices.Contains(cfg.Start["linux"], "-Fullscreen") {
		t.Fatalf("start: %v", cfg.Start)
	}
	if cfg.Components["ultrastar-deluxe"].Dir["windows"] != "{appdir}/game" || cfg.Content["theme"].Dir["linux"] == "" {
		t.Fatalf("components %v, content %v", cfg.Components, cfg.Content)
	}
	var notice *Shipped
	for i, s := range b.Shipped {
		if s.Rel == "licenses/ultrastar-deluxe.txt" {
			notice = &b.Shipped[i]
		}
	}
	if notice == nil || !strings.Contains(string(notice.Data), b.Game.Source) {
		t.Fatalf("no notice with the source code's address: %+v", b.Shipped)
	}

	// Windows: the merged launcher.json, the notice and the game are in the portable zip
	files, err := b.Windows(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	var portable string
	for _, f := range files {
		if strings.HasSuffix(f, "-windows-portable.zip") {
			portable = f
		}
	}
	names := zipNames(t, portable)
	var cfgText, noticeText string
	gameFound := false
	for name, body := range names {
		switch {
		case strings.HasSuffix(name, "/launcher.json") || name == "launcher.json":
			cfgText = body
		case strings.HasSuffix(name, "licenses/ultrastar-deluxe.txt"):
			noticeText = body
		case strings.HasSuffix(name, "game/game.exe"):
			gameFound = true
		}
	}
	if !strings.Contains(cfgText, "ultrastardx.exe") || noticeText == "" || !gameFound {
		t.Fatalf("portable zip: %v", slices.Sorted(func(yield func(string) bool) {
			for n := range names {
				if !yield(n) {
					return
				}
			}
		}))
	}

	// Flatpak: the game's module first (copied with its folder), then the launcher; the manifest's new fields
	path, err := b.FlatpakFiles(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	mods := m["modules"].([]any)
	if mods[0] != "games/ultrastar-deluxe/flatpak/ultrastardx.yml" || len(mods) != 2 {
		t.Fatalf("modules: %v", mods)
	}
	if _, err := os.Stat(filepath.Join(o.Out, "flatpak", "games", "ultrastar-deluxe", "flatpak", "ultrastardx.yml")); err != nil {
		t.Fatal(err)
	}
	if toStrings(m["sdk-extensions"])[0] != "org.freedesktop.Sdk.Extension.freepascal" || m["build-options"] == nil || m["cleanup"] == nil {
		t.Fatalf("manifest: %v", m)
	}
	data, _ = os.ReadFile(filepath.Join(o.Out, "flatpak", "files", "app", "launcher.json"))
	if !strings.Contains(string(data), "/app/bin/ultrastardx") || strings.Contains(string(data), `"components"`) {
		t.Fatalf("launcher.json in the Flatpak:\n%s", data)
	}

	// macOS: the game's app comes in a .dmg, which needs hdiutil (only on a Mac)
	if runtime.GOOS != "windows" {
		t.Setenv("PATH", t.TempDir())
		writeFile(t, filepath.Join(o.LauncherDir, "karaoke-launcher-darwin-arm64"), []byte("launcher program"))
		o.Signer = "-"
		if _, _, err := b.Macos(context.Background(), o); err == nil || !strings.Contains(err.Error(), "hdiutil") {
			t.Fatalf("macOS without hdiutil: %v", err)
		}
	}
}

func TestGameRefs(t *testing.T) {
	f, _ := usdxFixture(t)
	f.bundle["game"] = "no-such-game"
	if _, err := f.load(t); err == nil || !strings.Contains(err.Error(), "ultrastar-deluxe") {
		t.Fatalf("unknown game: %v", err)
	}
	f.bundle["game"] = 3
	if _, err := f.load(t); err == nil || !strings.Contains(err.Error(), `"game"`) {
		t.Fatalf("game 3: %v", err)
	}
	f.bundle["game"] = map[string]any{"path": "../outside"}
	if _, err := f.load(t); err == nil || !strings.Contains(err.Error(), "inside the folder") {
		t.Fatalf("path outside: %v", err)
	}

	// the community's own definition, next to bundle.json
	def := map[string]any{"game": 1, "id": "my-game", "name": "My Game", "version": "1.0", "license": "MIT",
		"homepage": "https://example.org/", "source": "https://example.org/src/",
		"bundle": map[string]any{"flatpak": map[string]any{"modules": []any{"m.yml"}}},
		"launcher": map[string]any{"start": map[string]any{"windows": []any{"mine/my.exe"}},
			"components": map[string]any{"ultrastar-deluxe": map[string]any{"dir": map[string]any{"windows": "{appdir}/mine"}}}}}
	data, _ := json.Marshal(def)
	writeFile(t, filepath.Join(f.dir, "mygame", "game.json"), data)
	f.bundle["game"] = map[string]any{"path": "mygame"}
	f.bundle["components"].(map[string]any)["ultrastar-deluxe"].(map[string]any)["version"] = "2.0"
	if _, err := f.load(t); err == nil || !strings.Contains(err.Error(), "m.yml") {
		t.Fatalf("missing module: %v", err)
	}
	writeFile(t, filepath.Join(f.dir, "mygame", "m.yml"), []byte("name: m\n"))
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	if b.Game.ID != "my-game" || b.Config.Start["windows"][0] != "mine/my.exe" || b.Game.Modules[0] != "m.yml" {
		t.Fatalf("own definition: %+v %v", b.Game, b.Config.Start)
	}

	def["license"] = ""
	data, _ = json.Marshal(def)
	writeFile(t, filepath.Join(f.dir, "mygame", "game.json"), data)
	if _, err := f.load(t); err == nil || !strings.Contains(err.Error(), "license") {
		t.Fatalf("no license: %v", err)
	}
}

func TestMergeJSON(t *testing.T) {
	var base, over map[string]any
	json.Unmarshal([]byte(`{"a": 1, "o": {"x": 1, "y": [1]}, "l": [1, 2], "flatpak": {"finish_args": ["--a", "--b"]},
		"components": {"g": {"keep": ["config.ini", "a.db"]}}}`), &base)
	json.Unmarshal([]byte(`{"a": 2, "o": {"y": [2], "z": 3}, "l": [3], "flatpak": {"finish_args": ["--b", "--c"]}, "n": null,
		"components": {"g": {"keep": ["mine.ini", "a.db"]}}}`), &over)
	base["f"] = map[string]any{"url": "https://example.org/a.zip", "sha256": "x", "strip": 1}
	over["f"] = map[string]any{"path": "a.zip", "sha256": "y"}
	got, _ := json.Marshal(mergeJSON(base, over, ""))
	want := `{"a":2,"components":{"g":{"keep":["config.ini","a.db","mine.ini"]}},"f":{"path":"a.zip","sha256":"y"},` +
		`"flatpak":{"finish_args":["--a","--b","--c"]},"l":[3],"n":null,"o":{"x":1,"y":[2],"z":3}}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// TestMacosHelperApp copies an app from a .dmg into the launcher's app with stand-ins for hdiutil, ditto and
// codesign: the app is in Contents/Helpers with its link, and it is signed before the launcher's app.
func TestMacosHelperApp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the macOS app is made on Linux or macOS")
	}
	f := newFixture(t)
	f.launcher["app_id"] = "org.example.Karaoke"
	delete(f.bundle["components"].(map[string]any), "game")
	delete(f.launcher, "components")
	dmg := []byte("a disk image")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(dmg) }))
	defer srv.Close()
	f.bundle["macos"] = map[string]any{"apps": []any{map[string]any{"url": srv.URL + "/game.dmg", "sha256": sum(dmg), "app": "Game.app"}}}
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "log")
	scripts := map[string]string{
		// attach -nobrowse -readonly -noautoopen -mountpoint <dir> <dmg>: the app with a program and a link
		"hdiutil": `case "$1" in
attach) m=$6; mkdir -p "$m/Game.app/Contents/MacOS" "$m/Game.app/Contents/Frameworks/A"
  printf game > "$m/Game.app/Contents/MacOS/game"; chmod 755 "$m/Game.app/Contents/MacOS/game"
  ln -s A "$m/Game.app/Contents/Frameworks/Current" ;;
detach) rm -rf "$3"/Game.app ;;
create) echo dmg > "$9" ;;
esac
echo "hdiutil $1" >> '` + log + `'
`,
		"ditto":    "cp -R \"$1\" \"$2\"\n",
		"codesign": "for a; do app=$a; done\necho \"codesign $(basename \"$app\")\" >> '" + log + "'\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	launchers := t.TempDir()
	writeFile(t, filepath.Join(launchers, "karaoke-launcher-darwin-arm64"), []byte("launcher program"))
	o := Options{Out: t.TempDir(), LauncherDir: launchers}
	files, signer, err := b.Macos(context.Background(), o)
	if err != nil || !strings.HasSuffix(signer, "codesign") || len(files) != 2 {
		t.Fatalf("%v %q %v", err, signer, files)
	}
	steps, _ := os.ReadFile(log)
	if want := "hdiutil attach\nhdiutil detach\ncodesign Game.app\ncodesign Example.app\nhdiutil create\n"; string(steps) != want {
		t.Fatalf("steps:\n%s\nwant:\n%s", steps, want)
	}
	z, err := zip.OpenReader(files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	found := 0
	for _, e := range z.File {
		switch e.Name {
		case "Example.app/Contents/Helpers/Game.app/Contents/MacOS/game":
			found++
			if e.Mode()&0o111 == 0 {
				t.Errorf("the game's program lost its executable bit: %v", e.Mode())
			}
		case "Example.app/Contents/Helpers/Game.app/Contents/Frameworks/Current":
			found++
			r, _ := e.Open()
			var target bytes.Buffer
			target.ReadFrom(r)
			r.Close()
			if e.Mode()&os.ModeSymlink == 0 || target.String() != "A" {
				t.Errorf("the link is not kept: %v %q", e.Mode(), target.String())
			}
		}
	}
	if found != 2 {
		t.Fatalf("the zip lacks the game's app")
	}
	if entries, _ := os.ReadDir(filepath.Join(o.Out, "macos")); len(entries) != 3 { // app/, the zip, the dmg: no mount folder left
		t.Errorf("left in macos/: %v", entries)
	}
}

// TestGameVersionOnly: a component's version and its archives are one unit. Changing only the version of a game
// definition's component would ship the definition's archives under the new number: refused. A new version with
// the community's own archives is fine.
func TestGameVersionOnly(t *testing.T) {
	f, _ := usdxFixture(t)
	own := f.bundle["components"].(map[string]any)["ultrastar-deluxe"].(map[string]any)["windows-amd64"]
	f.bundle["components"] = map[string]any{"ultrastar-deluxe": map[string]any{"version": "2026.12.0"}}
	if _, err := f.load(t); err == nil || !strings.Contains(err.Error(), "windows-amd64") {
		t.Fatalf("only the version changed: %v", err)
	}
	f.bundle["components"] = map[string]any{"ultrastar-deluxe": map[string]any{"version": "2026.12.0", "windows-amd64": own}}
	if _, err := f.load(t); err != nil {
		t.Fatalf("version with own archive: %v", err)
	}
}
