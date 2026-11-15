package bundle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kfeed "github.com/basisbit/karaoke-feeds/tool"
	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/update"
)

func newKey(t *testing.T) (file, pub string) {
	t.Helper()
	file = filepath.Join(t.TempDir(), "app.key")
	if kfeed.Run([]string{"kfeed", "keygen", file}, "") != 0 {
		t.Fatal("keygen")
	}
	_, pub, err := kfeed.Sign(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	return file, pub
}

func TestSite(t *testing.T) {
	f, o := windowsFixture(t)
	key, pub := newKey(t)
	// the site is served from <out>/site; the Linux game archive is on another server
	srv := httptest.NewServer(http.FileServer(http.Dir(filepath.Join(o.Out, "site"))))
	defer srv.Close()
	ext := t.TempDir()
	linuxGame := []byte("linux game archive")
	writeFile(t, filepath.Join(ext, "game-linux.tar.gz"), linuxGame)
	other := httptest.NewServer(http.FileServer(http.Dir(ext)))
	defer other.Close()
	f.bundle["site"] = srv.URL + "/"
	f.launcher["updates"] = map[string]any{"url": srv.URL + "/app.json", "key": pub}
	lin := component(f, "linux-amd64")
	lin["url"], lin["sha256"] = other.URL+"/game-linux.tar.gz", sum(linuxGame)
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Windows(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	notes, err := b.BuildSite(context.Background(), o, key)
	if err != nil || len(notes) != 0 {
		t.Fatalf("site: %v %v", notes, err)
	}
	for _, name := range []string{"download/example-1.0.0-windows-portable.zip", "files/launcher-1.0.0-windows-amd64.zip", "files/game-2.0-windows-amd64.zip"} {
		if _, err := os.Stat(filepath.Join(o.Out, "site", filepath.FromSlash(name))); err != nil {
			t.Errorf("site has no %s", name)
		}
	}

	// the launcher accepts app.json, offers both components and installs the game from the site
	base := t.TempDir()
	app, game := filepath.Join(base, "app"), filepath.Join(base, "app", "game")
	os.MkdirAll(app, 0o755)
	update.WriteState(app, update.State{Component: config.Launcher, Version: "0.9"})
	u := &update.Updater{URL: srv.URL + "/app.json", Key: pub, ID: "example", UserData: t.TempDir(), Platform: "windows-amd64",
		Targets: []update.Target{{ID: config.Launcher, Dir: app}, {ID: "game", Dir: game}}}
	res, err := u.Check(context.Background())
	if err != nil || len(res.Updates) != 2 {
		t.Fatalf("check: %+v %v", res, err)
	}
	for _, up := range res.Updates {
		if err := u.Stage(context.Background(), up, nil); err != nil {
			t.Fatalf("%s: %v", up.ID, err)
		}
	}
	if _, err := update.Apply(game, "game", nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(game, "game.exe")); string(data) != "game" {
		t.Fatalf("game.exe: %q", data)
	}
	// Linux: the game's archive stays on the other server, with the size the site measured
	u.Platform, u.Targets = "linux-amd64", []update.Target{{ID: "game", Dir: filepath.Join(base, "linux-game")}}
	if res, err := u.Check(context.Background()); err != nil || len(res.Updates) != 1 || res.Updates[0].File.Size != int64(len(linuxGame)) ||
		res.Updates[0].File.URL != other.URL+"/game-linux.tar.gz" {
		t.Fatalf("linux: %+v %v", res, err)
	}

	wrong, _ := newKey(t)
	if _, err := b.BuildSite(context.Background(), o, wrong); err == nil || !strings.Contains(err.Error(), `"updates.key"`) {
		t.Fatalf("wrong key: %v", err)
	}
	delete(f.launcher, "updates")
	if b, err = f.load(t); err != nil {
		t.Fatal(err)
	}
	if _, err := b.BuildSite(context.Background(), o, key); err == nil || !strings.Contains(err.Error(), `no "updates"`) {
		t.Fatalf("without updates: %v", err)
	}
}

func TestSiteFlatpak(t *testing.T) {
	f := newFixture(t)
	key, pub := newKey(t)
	f.launcher["app_id"] = "org.example.Karaoke"
	f.launcher["updates"].(map[string]any)["key"] = pub
	delete(f.bundle["components"].(map[string]any)["game"].(map[string]any), "linux-amd64") // no download
	b, err := f.load(t)
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Out: t.TempDir()}
	fp := filepath.Join(o.Out, "flatpak")
	writeFile(t, filepath.Join(fp, "repo", "config"), []byte("[core]"))
	writeFile(t, filepath.Join(fp, "repo", "objects", "a"), []byte("object"))
	os.MkdirAll(filepath.Join(fp, "repo", "refs"), 0o755)
	if err := os.Symlink("../objects/a", filepath.Join(fp, "repo", "refs", "link")); err != nil {
		t.Skip("no symbolic links here")
	}
	writeFile(t, filepath.Join(fp, "org.example.Karaoke.flatpak"), []byte("bundle"))
	writeFile(t, filepath.Join(fp, "org.example.Karaoke.flatpakref"), []byte("[Flatpak Ref]"))
	notes, err := b.BuildSite(context.Background(), o, key)
	if err != nil || len(notes) != 1 { // only: no launcher archive
		t.Fatalf("site: %v %v", notes, err)
	}
	site := filepath.Join(o.Out, "site")
	for _, name := range []string{"flatpak/repo/config", "flatpak/repo/objects/a", "download/org.example.Karaoke.flatpak", "download/org.example.Karaoke.flatpakref"} {
		if _, err := os.Stat(filepath.Join(site, filepath.FromSlash(name))); err != nil {
			t.Errorf("site has no %s", name)
		}
	}
	if l, err := os.Readlink(filepath.Join(site, "flatpak", "repo", "refs", "link")); err != nil || l != "../objects/a" {
		t.Errorf("link in the repository: %q %v", l, err)
	}
}
