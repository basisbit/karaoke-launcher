package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/basisbit/karaoke-launcher/internal/update"
)

// windowsFixture is a fixture whose game has a real zip for Windows, with a launcher program in a folder.
func windowsFixture(t *testing.T) (*fixture, Options) {
	f := newFixture(t)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string]string{"game-1/game.exe": "game", "game-1/data/level.txt": "1", "game-1/config.ini": "default"} {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	z.Close()
	writeFile(t, filepath.Join(f.dir, "game", "game.zip"), buf.Bytes())
	win := component(f, "windows-amd64")
	win["sha256"], win["strip"] = sum(buf.Bytes()), 1
	f.launcher["components"].(map[string]any)["game"].(map[string]any)["keep"] = []any{"config.ini"}
	launchers := t.TempDir()
	writeFile(t, filepath.Join(launchers, "karaoke-launcher-windows-amd64.exe"), []byte("launcher program"))
	return f, Options{Out: t.TempDir(), LauncherDir: launchers, Makensis: "-"}
}

func zipNames(t *testing.T, name string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(name)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	got := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		var b bytes.Buffer
		b.ReadFrom(r)
		r.Close()
		got[f.Name] = b.String()
	}
	return got
}

func TestWindows(t *testing.T) {
	f, o := windowsFixture(t)
	f.launcher["name"] = map[string]any{"de": "Nur deutsch", "en": "Tom's \"Best\" $Karaoke ${X} $(^Name)\nNight & Co\\"}
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("makensis"); err == nil {
		o.Makensis = ""
	}
	files, err := b.Windows(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(o.Out, "windows")
	portable := zipNames(t, filepath.Join(out, "example-1.0.0-windows-portable.zip"))
	for _, name := range []string{"example/karaoke-launcher.exe", "example/launcher.json", "example/assets/logo.svg",
		"example/docs/readme.txt", "example/.component-launcher.json", "example/game/game.exe",
		"example/game/data/level.txt", "example/game/config.ini", "example/game/.component-game.json"} {
		if _, ok := portable[name]; !ok {
			t.Errorf("portable zip has no %s: %v", name, slices.Sorted(maps.Keys(portable)))
		}
	}
	var st update.State
	json.Unmarshal([]byte(portable["example/game/.component-game.json"]), &st)
	if slices.Sort(st.Files); st.Version != "2.0" || !slices.Equal(st.Files, []string{"data/level.txt", "game.exe"}) {
		t.Errorf("game state (kept files are not listed): %+v", st)
	}
	json.Unmarshal([]byte(portable["example/.component-launcher.json"]), &st)
	if st.Version != "1.0.0" || !slices.Contains(st.Files, "karaoke-launcher.exe") || !slices.Contains(st.Files, "docs/more/b.txt") {
		t.Errorf("launcher state: %+v", st)
	}
	component := zipNames(t, filepath.Join(out, "launcher-1.0.0-windows-amd64.zip"))
	if want := []string{"assets/logo.svg", "docs/more/b.txt", "docs/readme.txt", "karaoke-launcher.exe", "launcher.json"}; !slices.Equal(slices.Sorted(maps.Keys(component)), want) {
		t.Errorf("launcher component: %v", slices.Sorted(maps.Keys(component)))
	}

	script, _ := os.ReadFile(filepath.Join(out, "installer.nsi"))
	for _, want := range []string{
		`Name "Tom's $\"Best$\" $$Karaoke $${X} $$(^Name)$\nNight & Co\" "Tom's $\"Best$\" $$Karaoke $${X} $$(^Name)$\nNight && Co\"`,
		`CreateShortcut "$SMPROGRAMS\Tom's -Best- $$Karaoke $${X} $$(^Name)-Night & Co-.lnk"`,
		`SetOutPath "$INSTDIR\game\data"`,
		`RequestExecutionLevel user`,
	} {
		if !strings.Contains(string(script), want) {
			t.Errorf("installer.nsi has no %s", want)
		}
	}
	if strings.Contains(string(script), "\nNight") {
		t.Error("a line break of the name reached the script")
	}
	if o.Makensis == "-" {
		t.Log("makensis not found: the setup program was not built")
		return
	}
	setup := filepath.Join(out, "example-1.0.0-windows-setup.exe")
	if fi, err := os.Stat(setup); err != nil || fi.Size() == 0 || !slices.Contains(files, setup) {
		t.Fatalf("setup program: %v %v", err, files)
	}
}

func TestWindowsRefused(t *testing.T) {
	cases := []struct {
		name   string
		change func(f *fixture)
		want   string
	}{
		{"not below appdir", func(f *fixture) {
			f.launcher["components"].(map[string]any)["game"] = map[string]any{"dir": map[string]any{"windows": "{home}/game"}}
		}, `must be "{appdir}/<folder>"`},
		{"appdir itself", func(f *fixture) {
			f.launcher["components"].(map[string]any)["game"] = map[string]any{"dir": map[string]any{"windows": "{appdir}"}}
		}, `must be "{appdir}/<folder>"`},
		{"shipped inside", func(f *fixture) {
			f.launcher["components"].(map[string]any)["game"] = map[string]any{"dir": map[string]any{"windows": "{appdir}/Docs"}}
		}, "inside the folder of component game"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, o := windowsFixture(t)
			c.change(f)
			b, err := f.load(t)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Windows(context.Background(), o); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
}
