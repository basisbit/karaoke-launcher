package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/library"
	"github.com/basisbit/karaoke-launcher/internal/settings"
)

// testApp builds the window for a config file with an empty library, on Fyne's test driver.
func testApp(t *testing.T, cfgPath, lang string) *App { return testAppFeeds(t, cfgPath, lang, nil) }

// testAppFeeds is testApp with other feeds than the config's (for example local feed folders).
func testAppFeeds(t *testing.T, cfgPath, lang string, feeds []library.Feed) *App {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	userdata := t.TempDir()
	set, _ := settings.Load(userdata, filepath.Join(t.TempDir(), "library"))
	set.Language = lang
	if feeds == nil {
		for _, f := range cfg.Feeds {
			feeds = append(feeds, library.Feed{ID: f.ID, Address: f.Address, Key: f.Key, Starter: f.Starter})
		}
	}
	lib, err := library.Open(userdata, set.Library, feeds, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lib.Close)
	var fa fyne.App
	uiWait(func() { fa = test.NewTempApp(t) })
	var a *App
	appDir := t.TempDir()
	uiWait(func() {
		a = New(fa, Options{Config: cfg, Settings: set, Library: lib, AppDir: appDir, UserData: userdata,
			Version: "test", GOOS: "linux", Do: func(f func()) { uiJobs <- f }})
	})
	return a
}

// uiJobs is the one UI goroutine of all tests: one job after another, like Fyne's real driver. One for all tests,
// because Fyne has global caches and late jobs of one test must not run beside the next test.
var uiJobs = func() chan func() {
	jobs := make(chan func(), 4096)
	go func() {
		for f := range jobs {
			f()
		}
	}()
	return jobs
}()

// uiWait runs f on the UI goroutine and waits for it.
func uiWait(f func()) {
	done := make(chan struct{})
	uiJobs <- func() { f(); close(done) }
	<-done
}

// onUI runs f on the app's UI goroutine and waits for it.
func onUI(_ *App, f func()) { uiWait(f) }

// shot saves the window to notes/screenshots/<name>.png when SHOTS=1 (for people to look at; not compared).
func shot(t *testing.T, a *App, name string) {
	t.Helper()
	if os.Getenv("SHOTS") == "" {
		return
	}
	var img image.Image
	onUI(a, func() {
		a.win.Resize(fyne.NewSize(620, 680))
		img = a.win.Canvas().Capture()
	})
	dir := filepath.Join("..", "..", "notes", "screenshots")
	os.MkdirAll(dir, 0o755)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
}

func TestHomeScreens(t *testing.T) {
	for _, look := range []string{"dark", "light"} {
		a := testApp(t, filepath.Join("..", "..", "examples", look, "launcher.json"), "en")
		onUI(a, func() {
			h := a.home
			if h == nil || h.play.Text != "Choose your songs first" || h.play.Disabled() {
				t.Errorf("%s: Play with an empty library", look)
				return
			}
			h.setNews("Karaoke night on Friday at 8 pm.\nBring your friends!")
		})
		shot(t, a, "m3-home-"+look)
		onUI(a, a.showSettings)
		shot(t, a, "m3-settings-"+look)
	}
	pub := makeFeed(t, map[string][]string{"Pop": {"a", "b"}, "Rock": {"c"}, "Duets": {"d"}})
	for _, look := range []string{"dark", "light"} {
		a := testAppFeeds(t, filepath.Join("..", "..", "examples", look, "launcher.json"), "en",
			[]library.Feed{{ID: "main", Address: pub, Starter: []string{"pop"}}})
		onUI(a, a.showSongs)
		eventually(t, a, "needs line", func() bool { return a.songs != nil && a.songs.needs.Text != "" })
		shot(t, a, "m3-songs-"+look)
	}
	// a system without a start command: Play is replaced by a hint
	a := testApp(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "de")
	onUI(a, func() {
		a.o.GOOS = "windows"
		a.showHome()
		if a.home.play.Visible() || !a.home.hint.Visible() || a.home.hint.Text != "Das Spiel kann auf diesem System nicht gestartet werden." {
			t.Errorf("no start command: play %v, hint %q", a.home.play.Visible(), a.home.hint.Text)
		}
	})
}
