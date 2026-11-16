package bundle

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func flatpakFixture(t *testing.T) (*fixture, Options) {
	f := newFixture(t)
	f.launcher["app_id"] = "org.example.Karaoke"
	f.launcher["name"] = map[string]any{"en": "Sing & <Shout>\n\"now\"", "de": " Singen"}
	f.bundle["developer"] = `Tom "&" <Co>`
	f.bundle["description"] = "First paragraph with <tags> & more.\n\nSecond   one."
	writeFile(t, filepath.Join(f.dir, "flatpak", "game.yml"), []byte("name: game\nbuildsystem: simple\nbuild-commands: []\n"))
	f.bundle["flatpak"] = map[string]any{"runtime": "org.freedesktop.Platform", "runtime_version": "26.08",
		"sdk": "org.freedesktop.Sdk", "finish_args": []any{"--share=network"}, "modules": []any{"flatpak/game.yml"}}
	launchers := t.TempDir()
	writeFile(t, filepath.Join(launchers, "karaoke-launcher-linux-amd64"), []byte("launcher program"))
	return f, Options{Out: t.TempDir(), LauncherDir: launchers}
}

func TestFlatpakFiles(t *testing.T) {
	f, o := flatpakFixture(t)
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	path, err := b.FlatpakFiles(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(o.Out, "flatpak")
	var m map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &m); err != nil || filepath.Base(path) != "org.example.Karaoke.json" {
		t.Fatalf("manifest %s: %v", path, err)
	}
	if m["id"] != "org.example.Karaoke" || m["runtime-version"] != "26.08" || m["command"] != "karaoke-launcher" {
		t.Fatalf("manifest: %v", m)
	}
	modules := m["modules"].([]any)
	if rel, _ := modules[0].(string); rel == "" || !strings.HasSuffix(rel, "flatpak/game.yml") {
		t.Fatalf("community module: %v", modules[0])
	} else if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("module path does not resolve from the manifest: %v", err)
	}
	if cmds := strings.Join(toStrings(modules[1].(map[string]any)["build-commands"]), "\n"); !strings.Contains(cmds, "/app/share/icons/hicolor/512x512/apps/org.example.Karaoke.png") ||
		!strings.Contains(cmds, "ln -sf /app/lib/example/karaoke-launcher /app/bin/karaoke-launcher") {
		t.Fatalf("build commands:\n%s", cmds)
	}

	// launcher.json in the Flatpak has no updates and no components; the rest stays
	var cfg map[string]any
	data, _ = os.ReadFile(filepath.Join(out, "files", "app", "launcher.json"))
	json.Unmarshal(data, &cfg)
	if _, ok := cfg["updates"]; ok || cfg["components"] != nil || cfg["id"] != "example" || cfg["app_id"] != "org.example.Karaoke" {
		t.Fatalf("launcher.json in the Flatpak: %v", cfg)
	}
	for _, name := range []string{"app/karaoke-launcher", "app/assets/logo.svg", "app/docs/readme.txt", "icons/64.png", "icons/512.png"} {
		if _, err := os.Stat(filepath.Join(out, "files", filepath.FromSlash(name))); err != nil {
			t.Errorf("files/%s missing", name)
		}
	}

	desktop, _ := os.ReadFile(filepath.Join(out, "files", "org.example.Karaoke.desktop"))
	for _, want := range []string{`Name=Sing & <Shout>\n"now"`, `Name[de]=\sSingen`, "Icon=org.example.Karaoke", "Exec=karaoke-launcher"} {
		if !strings.Contains(string(desktop), want+"\n") {
			t.Errorf("desktop file has no %s:\n%s", want, desktop)
		}
	}
	meta, _ := os.ReadFile(filepath.Join(out, "files", "org.example.Karaoke.metainfo.xml"))
	var c struct {
		ID         string   `xml:"id"`
		Name       string   `xml:"name"`
		Paragraphs []string `xml:"description>p"`
		Launchable string   `xml:"launchable"`
		Developer  string   `xml:"developer>name"`
		Releases   []struct {
			Version string `xml:"version,attr"`
		} `xml:"releases>release"`
	}
	if err := xml.Unmarshal(meta, &c); err != nil {
		t.Fatalf("metainfo is no XML: %v\n%s", err, meta)
	}
	if c.ID != "org.example.Karaoke" || c.Name != "Sing & <Shout>\n\"now\"" || c.Launchable != "org.example.Karaoke.desktop" ||
		len(c.Paragraphs) != 2 || c.Paragraphs[0] != "First paragraph with <tags> & more." || c.Paragraphs[1] != "Second one." ||
		c.Developer != `Tom "&" <Co>` || len(c.Releases) != 1 || c.Releases[0].Version != "1.0.0" {
		t.Fatalf("metainfo: %+v", c)
	}
}

func toStrings(v any) []string {
	var l []string
	for _, s := range v.([]any) {
		l = append(l, s.(string))
	}
	return l
}

// TestFlatpakValidators runs the validators of the desktop file and the metainfo file when they are installed.
func TestFlatpakValidators(t *testing.T) {
	f, o := flatpakFixture(t)
	f.launcher["name"] = map[string]any{"en": "Karaoke", "de": "Karaoke"}
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.FlatpakFiles(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	files := filepath.Join(o.Out, "flatpak", "files")
	ran := 0
	for _, c := range [][]string{
		{"desktop-file-validate", filepath.Join(files, "org.example.Karaoke.desktop")},
		{"appstreamcli", "validate", "--no-net", filepath.Join(files, "org.example.Karaoke.metainfo.xml")},
	} {
		if _, err := exec.LookPath(c[0]); err != nil {
			t.Logf("%s not installed: not run", c[0])
			continue
		}
		ran++
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", c[0], err, out)
		} else if len(out) > 0 {
			t.Logf("%s:\n%s", c[0], out)
		}
	}
	if ran == 0 {
		t.Skip("no validator installed")
	}
}

func TestFlatpakProblems(t *testing.T) {
	for name, change := range map[string]func(f *fixture){
		"no app_id": func(f *fixture) { delete(f.launcher, "app_id") },
		"runtime":   func(f *fixture) { f.bundle["flatpak"].(map[string]any)["runtime"] = "a b" },
		"finish arg": func(f *fixture) {
			f.bundle["flatpak"].(map[string]any)["finish_args"] = []any{"--share=network --device=all"}
		},
		"module outside": func(f *fixture) { f.bundle["flatpak"].(map[string]any)["modules"] = []any{"../game.yml"} },
		"module missing": func(f *fixture) { f.bundle["flatpak"].(map[string]any)["modules"] = []any{"flatpak/nothing.yml"} },
		"unknown field":  func(f *fixture) { f.bundle["flatpak"].(map[string]any)["finishargs"] = []any{} },
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := flatpakFixture(t)
			change(f)
			if _, err := f.load(t); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
