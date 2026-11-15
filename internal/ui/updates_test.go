package ui

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	kfeed "github.com/basisbit/karaoke-feeds/tool"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/update"
)

// updateSite serves a signed app.json with one component per entry of files (component -> file name -> content).
// Archives wait for release to be closed before they are sent (nil: no waiting).
func updateSite(t *testing.T, versions map[string]string, files map[string]map[string]string, platform string, release chan struct{}) *update.Updater {
	t.Helper()
	dir := t.TempDir()
	key := filepath.Join(t.TempDir(), "app.key")
	if kfeed.Run([]string{"kfeed", "keygen", key}, "") != 0 {
		t.Fatal("keygen")
	}
	comps := map[string]update.Component{}
	for id, content := range files {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for name, body := range content {
			w, _ := z.Create(name)
			w.Write([]byte(body))
		}
		z.Close()
		os.WriteFile(filepath.Join(dir, id+".zip"), buf.Bytes(), 0o644)
		sum := sha256.Sum256(buf.Bytes())
		f := update.File{URL: id + ".zip", SHA256: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(buf.Len())}
		comps[id] = update.Component{Version: versions[id], Files: map[string]update.File{platform: f}}
	}
	data, _ := json.Marshal(update.Manifest{App: 1, ID: "test", Sequence: 1, Components: comps})
	os.WriteFile(filepath.Join(dir, "app.json"), data, 0o644)
	sig, pub, err := kfeed.Sign(key, data)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	os.MkdirAll(filepath.Join(dir, "sig"), 0o755)
	os.WriteFile(filepath.Join(dir, "sig", hex.EncodeToString(sum[:])+".sig"), []byte(sig), 0o644)
	files_ := http.FileServer(http.Dir(dir))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if release != nil && strings.HasSuffix(r.URL.Path, ".zip") {
			<-release
		}
		files_.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return &update.Updater{URL: srv.URL + "/app.json", Key: pub, ID: "test", UserData: t.TempDir(), Platform: platform}
}

func updateApp(t *testing.T, lang string, u *update.Updater) *App {
	a := testApp(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), lang)
	a.o.Updater = u
	a.o.Config.ID = "test" // the id updateSite puts into app.json
	return a
}

func TestUpdateLineHidden(t *testing.T) {
	a := testApp(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en")
	onUI(a, func() {
		if a.o.Updater != nil || a.home.updRow.Visible() {
			t.Error("update row shown without updates")
		}
	})
}

func TestUpdateGame(t *testing.T) {
	release := make(chan struct{})
	u := updateSite(t, map[string]string{"game": "2.0"}, map[string]map[string]string{"game": {"game.bin": "game 2.0"}}, "test-x", release)
	game := filepath.Join(t.TempDir(), "game")
	u.Targets = []update.Target{{ID: "game", Dir: game}}
	a := updateApp(t, "en", u)
	a.o.Running = func(string) ([]string, bool) { return nil, true }
	go a.runUpdates()
	eventually(t, a, "Play disabled while the game is prepared", func() bool {
		return a.home.updRow.Visible() && a.home.updText.Text == "Getting the game ready …" && a.home.play.Disabled() && !a.home.updBtn.Visible()
	})
	close(release)
	eventually(t, a, "asked to close the game, Play still disabled", func() bool { return a.closeAsk != nil && a.home.play.Disabled() })
	if _, err := os.Stat(filepath.Join(game, "game.bin")); err == nil {
		t.Fatal("installed before the user closed the game")
	}
	shot(t, a, "m5-home-update-close-game")
	onUI(a, func() { a.closeAsk.Confirm() })
	eventually(t, a, "game installed, Play enabled again", func() bool {
		return !a.home.updRow.Visible() && !a.home.play.Disabled()
	})
	if data, err := os.ReadFile(filepath.Join(game, "game.bin")); err != nil || string(data) != "game 2.0" {
		t.Fatalf("game file: %q %v", data, err)
	}
	if st, _ := update.Installed(game, "game"); st == nil || st.Version != "2.0" {
		t.Fatalf("state: %+v", st)
	}
}

func TestUpdateLauncher(t *testing.T) {
	u := updateSite(t, map[string]string{"launcher": "2.0"}, map[string]map[string]string{"launcher": {"launcher.bin": "2.0"}}, "test-x", nil)
	app := t.TempDir()
	update.WriteState(app, update.State{Component: config.Launcher, Version: "1.0"})
	u.Targets = []update.Target{{ID: config.Launcher, Dir: app}}
	a := updateApp(t, "en", u)
	relaunched := make(chan struct{}, 1)
	a.o.Relaunch = func() error { relaunched <- struct{}{}; return nil }
	go a.runUpdates()
	eventually(t, a, "update ready with a restart button", func() bool {
		return a.home.updText.Text == "An update is ready." && a.home.updBtn.Visible() && a.home.updBtn.Text == "Restart now" && !a.home.play.Disabled()
	})
	shot(t, a, "m5-home-update-ready")
	onUI(a, func() { a.home.updBtn.OnTapped() })
	<-relaunched
	if st, _ := update.Installed(app, config.Launcher); st == nil || st.Version != "2.0" {
		t.Fatalf("launcher state after restart: %+v", st)
	}
}

func TestUpdateGameClose(t *testing.T) {
	u := updateSite(t, map[string]string{"game": "2.0"}, map[string]map[string]string{"game": {"game.bin": "game 2.0"}}, "test-x", nil)
	game := filepath.Join(t.TempDir(), "game")
	u.Targets = []update.Target{{ID: "game", Dir: game}}
	a := updateApp(t, "en", u)
	var checks atomic.Int32
	a.o.Running = func(dir string) ([]string, bool) {
		if dir != game {
			t.Errorf("checked %q", dir)
		}
		if checks.Add(1) == 1 {
			return []string{"game.exe"}, true // still open after the user said it is closed
		}
		return nil, true
	}

	// "Later": nothing is installed, the download waits, Play works
	done := make(chan struct{})
	go func() { a.runUpdates(); close(done) }()
	eventually(t, a, "question", func() bool { return a.closeAsk != nil })
	onUI(a, func() { a.closeAsk.Dismiss() })
	<-done
	onUI(a, func() {
		if a.home.play.Disabled() || a.home.updRow.Visible() {
			t.Error("Play disabled or update row shown after Later")
		}
	})
	if _, err := os.Stat(filepath.Join(game, "game.bin")); err == nil || !update.Pending(game, "game") || checks.Load() != 0 {
		t.Fatalf("after Later: installed %v, pending %v, checks %d", err == nil, update.Pending(game, "game"), checks.Load())
	}

	// next run: the waiting download is offered first; the game still runs once, then it is installed
	go a.runUpdates()
	eventually(t, a, "question again", func() bool { return a.closeAsk != nil })
	onUI(a, func() { a.closeAsk.Confirm() })
	eventually(t, a, "asked again while the game runs", func() bool { return checks.Load() == 1 && a.closeAsk != nil })
	if update.Pending(game, "game") == false {
		t.Fatal("installed while the game runs")
	}
	onUI(a, func() { a.closeAsk.Confirm() })
	eventually(t, a, "installed", func() bool { return !a.home.play.Disabled() && !update.Pending(game, "game") })
	if checks.Load() != 2 {
		t.Fatalf("%d checks, want 2", checks.Load())
	}
	if data, _ := os.ReadFile(filepath.Join(game, "game.bin")); string(data) != "game 2.0" {
		t.Fatalf("game file: %q", data)
	}
}

func TestUpdateRestartFails(t *testing.T) {
	u := updateSite(t, map[string]string{"launcher": "2.0"}, map[string]map[string]string{"launcher": {"launcher.bin": "2.0"}}, "test-x", nil)
	app := t.TempDir()
	update.WriteState(app, update.State{Component: config.Launcher, Version: "1.0"})
	u.Targets = []update.Target{{ID: config.Launcher, Dir: app}}
	a := updateApp(t, "en", u)
	a.o.Relaunch = func() error { return errors.New("exec failed") }
	go a.runUpdates()
	eventually(t, a, "restart button", func() bool { return a.home.updBtn.Visible() })
	onUI(a, func() { a.home.updBtn.OnTapped() })
	eventually(t, a, "error shown", func() bool { return a.win.Canvas().Overlays().Top() != nil })
	if st, _ := update.Installed(app, config.Launcher); st == nil || st.Version != "2.0" {
		t.Fatalf("launcher state: %+v", st)
	}
}

func TestUpdateNotice(t *testing.T) {
	u := updateSite(t, map[string]string{"launcher": "2.0"}, map[string]map[string]string{"launcher": {"launcher.bin": "2.0"}}, "other-arch", nil)
	app := t.TempDir()
	update.WriteState(app, update.State{Component: config.Launcher, Version: "1.0"})
	u.Targets = []update.Target{{ID: config.Launcher, Dir: app}}
	u.Platform = runtime.GOOS + "-" + runtime.GOARCH
	for _, tc := range []struct{ lang, text, button string }{
		{"en", "A new version is available.", "Get it"},
		{"de", "Eine neue Version ist verfügbar.", "Herunterladen"},
	} {
		a := updateApp(t, tc.lang, u)
		a.o.Config.Updates = &config.Updates{Page: "https://example.org/karaoke/"}
		os.Remove(filepath.Join(u.UserData, "update.json"))
		go a.runUpdates()
		eventually(t, a, tc.lang+" notice", func() bool {
			return a.home.updText.Text == tc.text && a.home.updBtn.Visible() && a.home.updBtn.Text == tc.button
		})
		if tc.lang == "en" {
			shot(t, a, "m5-home-update-notice")
		}
	}
}
