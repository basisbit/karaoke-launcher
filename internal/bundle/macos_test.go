package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// plistKeys reads the top-level dict of an Info.plist: key -> text of its value (true/false for <true/> and <false/>).
func plistKeys(t *testing.T, data []byte) map[string]string {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(data))
	keys, depth, key, text := map[string]string{}, 0, "", ""
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return keys
		}
		if err != nil {
			t.Fatalf("Info.plist: %v", err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			depth++
			text = ""
			if depth == 3 && (e.Name.Local == "true" || e.Name.Local == "false") {
				keys[key] = e.Name.Local
			}
		case xml.CharData:
			text += string(e)
		case xml.EndElement:
			if depth == 3 && e.Name.Local == "key" {
				key = text
			} else if depth == 3 && e.Name.Local == "string" {
				keys[key] = text
			}
			depth--
		}
	}
}

func TestMacos(t *testing.T) {
	f := newFixture(t)
	f.launcher["app_id"] = "org.example.Kara_oke"
	f.launcher["name"] = map[string]any{"en": "Tom & <Co>"}
	f.bundle["version"] = "1.2.0-beta"
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "game-1/game"}
	h.SetMode(0o755)
	w, _ := z.CreateHeader(h)
	w.Write([]byte("mac game"))
	z.Close()
	writeFile(t, filepath.Join(f.dir, "game", "game-mac.zip"), buf.Bytes())
	f.bundle["components"].(map[string]any)["game"].(map[string]any)["darwin-arm64"] = map[string]any{"path": "game/game-mac.zip", "sha256": sum(buf.Bytes()), "strip": 1}
	dirs := f.launcher["components"].(map[string]any)["game"].(map[string]any)
	dirs["dir"] = map[string]any{"windows": "{appdir}/game", "macos": "{userdata}/game"} // installed at the first start
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	launchers := t.TempDir()
	writeFile(t, filepath.Join(launchers, "karaoke-launcher-darwin-arm64"), []byte("launcher program"))
	o := Options{Out: t.TempDir(), LauncherDir: launchers, Signer: "-"}
	files, signer, err := b.Macos(context.Background(), o)
	if runtime.GOOS == "windows" {
		if err != errNoExecBit {
			t.Fatalf("on Windows: %v", err)
		}
		t.Skip("the macOS app is made on Linux or macOS")
	}
	if err != nil || signer != "" {
		t.Fatal(err, signer)
	}
	want := filepath.Join(o.Out, "macos", "example-1.2.0-beta-macos-arm64.zip")
	if len(files) != 1 || files[0] != want { // no hdiutil here
		t.Fatalf("files: %v", files)
	}
	zr, err := zip.OpenReader(want)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	entries := map[string]*zip.File{}
	for _, e := range zr.File {
		entries[e.Name] = e
	}
	app := "Tom & -Co-.app/Contents/"
	expect := map[string]bool{"MacOS/karaoke-launcher": true, "Resources/launcher.json": false, // name -> executable
		"Resources/.component-launcher.json": false, "Resources/docs/readme.txt": false, "Resources/docs/more/b.txt": false,
		"Resources/assets/logo.svg": false, "Resources/icon.icns": false, "Info.plist": false}
	for name, exec := range expect {
		e := entries[app+name]
		if e == nil {
			t.Errorf("zip has no %s", app+name)
			continue
		}
		if got := e.Mode().Perm()&0o111 != 0; got != exec {
			t.Errorf("%s: mode %v", name, e.Mode())
		}
	}
	for name := range entries {
		if _, ok := expect[strings.TrimPrefix(name, app)]; !ok {
			t.Errorf("zip has %s (MacOS may only hold the program; the game is installed at the first start)", name)
		}
	}
	if e := entries[app+"Resources/icon.icns"]; e != nil {
		r, _ := e.Open()
		head := make([]byte, 4)
		io.ReadFull(r, head)
		r.Close()
		if string(head) != "icns" {
			t.Errorf("icon.icns starts with %q", head)
		}
	}
	r, _ := entries[app+"Info.plist"].Open()
	data, _ := io.ReadAll(r)
	r.Close()
	keys := plistKeys(t, data)
	for k, v := range map[string]string{"CFBundleIdentifier": "org.example.Kara-oke", "CFBundleName": "Tom & <Co>",
		"CFBundleExecutable": "karaoke-launcher", "CFBundlePackageType": "APPL", "CFBundleShortVersionString": "1.2.0",
		"CFBundleVersion": "1.2.0", "CFBundleIconFile": "icon.icns", "LSMinimumSystemVersion": minMacOS,
		"NSHighResolutionCapable": "true"} {
		if keys[k] != v {
			t.Errorf("%s = %q, want %q", k, keys[k], v)
		}
	}

	// a component in the app is refused: the app must not change itself
	dirs["dir"] = map[string]any{"macos": "{appdir}/game"}
	if b, err = f.load(t); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Macos(context.Background(), o); err == nil || !strings.Contains(err.Error(), "{userdata}/game") {
		t.Fatalf("component in the app: %v", err)
	}
	delete(f.launcher, "app_id")
	if b, err = f.load(t); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Macos(context.Background(), o); err == nil || !strings.Contains(err.Error(), "app_id") {
		t.Fatalf("without app_id: %v", err)
	}
}

// TestMacosSign signs with stand-ins for codesign and rcodesign: the signature must be in the zip.
func TestMacosSign(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the macOS app is made on Linux or macOS")
	}
	f := newFixture(t)
	f.launcher["app_id"] = "org.example.Karaoke"
	delete(f.bundle["components"].(map[string]any), "game")
	delete(f.launcher, "components")
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	launchers := t.TempDir()
	writeFile(t, filepath.Join(launchers, "karaoke-launcher-darwin-arm64"), []byte("launcher program"))
	for name, args := range map[string]string{"codesign": "--force --sign - ", "rcodesign": "sign "} {
		bin := t.TempDir()
		log := filepath.Join(bin, "args")
		script := "#!/bin/sh\necho \"$*\" > '" + log + "'\nfor a; do app=$a; done\nmkdir -p \"$app/Contents/_CodeSignature\" && echo sealed > \"$app/Contents/_CodeSignature/CodeResources\"\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		o := Options{Out: t.TempDir(), LauncherDir: launchers, Signer: filepath.Join(bin, name)}
		files, signer, err := b.Macos(context.Background(), o)
		if err != nil || signer != o.Signer {
			t.Fatalf("%s: %v %q", name, err, signer)
		}
		got, _ := os.ReadFile(log)
		if want := args + filepath.Join(o.Out, "macos", "app", "Example.app") + "\n"; string(got) != want {
			t.Errorf("%s called with %q, want %q", name, got, want)
		}
		if zipNames(t, files[0])["Example.app/Contents/_CodeSignature/CodeResources"] != "sealed\n" {
			t.Errorf("%s: the signature is not in the zip", name)
		}
	}
}

func TestMacVersion(t *testing.T) {
	for in, want := range map[string]string{"1.2.0": "1.2.0", "1.2.0-beta": "1.2.0", "2": "2", "v1.0": "0", "3.1.": "3.1", "10.4.1+git": "10.4.1"} {
		if got := macVersion(in); got != want {
			t.Errorf("macVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
