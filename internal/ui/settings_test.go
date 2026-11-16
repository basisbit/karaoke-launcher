package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	kfeed "github.com/basisbit/karaoke-feeds/tool"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/folders"
	"github.com/basisbit/karaoke-launcher/internal/library"
	"github.com/basisbit/karaoke-launcher/internal/settings"
)

// find returns the first widget of type T in o for which match is true.
func find[T fyne.CanvasObject](o fyne.CanvasObject, match func(T) bool) (T, bool) {
	var zero T
	if w, ok := o.(T); ok && match(w) {
		return w, true
	}
	switch c := o.(type) {
	case *fyne.Container:
		for _, x := range c.Objects {
			if w, ok := find(x, match); ok {
				return w, true
			}
		}
	case fyne.Widget:
		if r := test.WidgetRenderer(c); r != nil {
			for _, x := range r.Objects() {
				if w, ok := find(x, match); ok {
					return w, true
				}
			}
		}
	}
	return zero, false
}

func button(o fyne.CanvasObject, text string) *widget.Button {
	b, _ := find(o, func(b *widget.Button) bool { return b.Text == text })
	return b
}

func TestSettingsScreen(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a"}})
	srv := httptest.NewServer(http.FileServer(http.Dir(pub)))
	defer srv.Close()
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en", []library.Feed{})
	onUI(a, a.showSettings)
	shot(t, a, "m3-settings-dark")

	// licence filter: saved
	onUI(a, func() {
		c, ok := find(a.win.Content(), func(c *widget.Check) bool { return true }) // the first one is NC
		if !ok {
			t.Error("no NC checkbox")
			return
		}
		c.SetChecked(true)
	})
	if s2, _ := settings.Load(a.o.UserData, ""); !slices.Equal(s2.Exclude, []string{"nc"}) {
		t.Fatalf("exclude saved: %v", s2.Exclude)
	}

	// add a song source: confirmation with name and operator, then it is in the settings and the library
	onUI(a, func() {
		e, _ := find(a.win.Content(), func(e *widget.Entry) bool { return true })
		e.SetText(srv.URL + "/")
		button(a.win.Content(), "Add …").OnTapped()
	})
	eventually(t, a, "add confirmation", func() bool { return len(a.win.Canvas().Overlays().List()) > 0 })
	shot(t, a, "m3-settings-add")
	onUI(a, func() {
		top := a.win.Canvas().Overlays().Top()
		if _, ok := find(top, func(l *widget.Label) bool { return l.Text == "Test Feed\nRun by Tester\n"+a.tr.T("songs.unsigned") }); !ok {
			t.Error("confirmation does not show the feed")
		}
		button(top, "Add …").OnTapped()
	})
	added := library.AddedID(srv.URL + "/")
	hasAdded := func() bool {
		return slices.ContainsFunc(a.o.Library.Feeds(), func(f library.Feed) bool { return f.ID == added })
	}
	eventually(t, a, "source added", func() bool { return len(a.o.Settings.Feeds) == 1 && hasAdded() })

	// remove it again
	onUI(a, func() { button(a.win.Content(), "Remove").OnTapped() })
	eventually(t, a, "remove confirmation", func() bool { return len(a.win.Canvas().Overlays().List()) > 0 })
	onUI(a, func() { button(a.win.Canvas().Overlays().Top(), "Remove").OnTapped() })
	eventually(t, a, "source removed", func() bool { return len(a.o.Settings.Feeds) == 0 && !hasAdded() })

	// language: German texts everywhere (also the window title), saved
	a.o.Config.Name = config.Text{"en": "Sing", "de": "Singen"}
	onUI(a, func() {
		s, _ := find(a.win.Content(), func(s *widget.Select) bool { return true })
		s.SetSelected("Deutsch")
	})
	onUI(a, func() {
		if button(a.win.Content(), "Zurück") == nil {
			t.Error("no German texts after choosing Deutsch")
		}
		if a.win.Title() != "Singen" {
			t.Errorf("window title %q after choosing Deutsch", a.win.Title())
		}
	})
	if s2, _ := settings.Load(a.o.UserData, ""); s2.Language != "de" {
		t.Fatalf("language saved: %q", s2.Language)
	}
	shot(t, a, "m3-settings-de")

	onUI(a, func() { button(a.win.Content(), "Über").OnTapped() })
	eventually(t, a, "about screen with the licence texts", func() bool {
		_, ok := find(a.win.Content(), func(l *widget.Label) bool { return strings.Contains(l.Text, "Copyright 2009 The Go Authors") })
		return ok
	})
	shot(t, a, "m3-about-de")
	onUI(a, func() { button(a.win.Content(), "Zurück").OnTapped() })
	onUI(a, func() {
		if button(a.win.Content(), "Über") == nil {
			t.Error("Back from About does not lead to the settings")
		}
	})
}

func TestImportFolders(t *testing.T) {
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en", []library.Feed{})
	old := t.TempDir()
	onUI(a, func() {
		a.o.Settings.Import = []string{old}
		a.showSettings()
		if _, ok := find(a.win.Content(), func(l *widget.Label) bool { return l.Text == old }); !ok {
			t.Error("import folder not listed")
		}
		if button(a.win.Content(), "Add folder …") == nil {
			t.Error("no Add folder button")
		}
	})
	shot(t, a, "m4-settings-import")
	// the first "Remove" on the screen belongs to the import folder (the sources come later)
	onUI(a, func() { button(a.win.Content(), "Remove").OnTapped() })
	if s2, _ := settings.Load(a.o.UserData, ""); len(s2.Import) != 0 || len(a.o.Settings.Import) != 0 {
		t.Fatalf("import folder not removed: %v", s2.Import)
	}
}

func TestSongFolderChoice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a := testApp(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en")
	a.o.AppDir = filepath.Join(t.TempDir(), "app")
	a.o.GOOS = "linux" // the example's theme folder on Linux is {home}/.karaoke-game/themes
	base := t.TempDir()
	marked := func(name, purpose string) string {
		d := filepath.Join(base, name)
		os.MkdirAll(d, 0o755)
		folders.Mark(d, purpose, "# x")
		return d
	}
	songs := marked("songs", folders.Songs)
	program := filepath.Join(base, "program")
	os.MkdirAll(program, 0o755)
	os.WriteFile(filepath.Join(program, ".component-game.json"), []byte("{}"), 0o644)
	for dir, ok := range map[string]bool{
		filepath.Join(base, "new"): true,
		songs:                      true, // the song folder used before
		marked("themes", folders.Content("theme")): false,
		filepath.Join(songs, "main"):               false, // inside another song folder
		program:                                    false,
		a.o.AppDir:                                 false,
		filepath.Dir(a.o.AppDir):                   false,
		filepath.Join(a.o.AppDir, "songs"):         true,  // a portable installation
		home:                                       false, // holds the theme folder
		filepath.Join(home, ".karaoke-game", "themes"): false,
	} {
		if got := a.songFolderOK(dir); got != ok {
			t.Errorf("songFolderOK(%s) = %v, want %v", dir, got, ok)
		}
	}
}

// TestExcludeDuringDownload: a licence filter chosen while a download runs is applied (the download is paused for
// it and continues with the filter), not dropped.
func TestExcludeDuringDownload(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a"}, "Rock": {"c"}})
	nd := `{"licenses": {"*": {"spdx": "CC-BY-ND-4.0", "attribution": "Test Artist", "source": "https://example.org/src"}}}`
	src := filepath.Dir(pub)
	os.WriteFile(filepath.Join(src, "songs", "Pop", "a", "feed-meta.json"), []byte(nd), 0o644)
	old := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	code := kfeed.Run([]string{"kfeed", "build", src}, "test")
	os.Stdout.Close()
	os.Stdout = old
	if code != 0 {
		t.Fatal("kfeed build failed")
	}
	var stall atomic.Bool
	files := http.FileServer(http.Dir(pub))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stall.Load() && strings.HasPrefix(r.URL.Path, "/blobs/") {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer srv.Close()
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en", []library.Feed{{ID: "main", Address: srv.URL + "/"}})
	lib := a.o.Library
	lib.Start(map[string]library.Choice{"main": {Packs: []string{"pop"}}})
	eventually(t, a, "pop downloaded", func() bool { s := lib.Status(); return !s.Running && s.Results["main"] != nil })
	if r := lib.Status().Results["main"]; len(r.Added) != 1 {
		t.Fatalf("first download: %+v", r)
	}
	stall.Store(true)
	lib.Start(map[string]library.Choice{"main": {Packs: []string{"pop", "rock"}}})
	eventually(t, a, "rock downloading", func() bool { return lib.Status().Running })
	stall.Store(false) // new requests are served; the running one hangs until the download is paused
	onUI(a, func() { a.o.Settings.Exclude = []string{"nd"}; a.applyExclude() })
	eventually(t, a, "the download with the filter ended", func() bool {
		s := lib.Status()
		return !s.Running && s.Results["main"] != nil && len(s.Results["main"].Added) == 1 // rock
	})
	if entries, _ := os.ReadDir(lib.Dir("main")); slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.HasSuffix(e.Name(), "-a") }) {
		t.Fatalf("the ND song is still there: the filter was not applied (%v)", entries)
	}
}

// TestSettingsDialogKeepsScreen: answering a settings dialog after going back to Home does not jump to Settings.
func TestSettingsDialogKeepsScreen(t *testing.T) {
	a := testApp(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en")
	feed := settings.Feed{Address: "https://example.org/feed/"}
	a.o.Settings.Feeds = []settings.Feed{feed}
	onUI(a, a.showSettings)
	onUI(a, func() { a.removeSource(feed) })
	var confirm *widget.Button
	onUI(a, func() { confirm = button(a.win.Canvas().Overlays().Top(), "Remove") })
	onUI(a, a.showHome) // the dialog stays open; the user went back
	onUI(a, func() { confirm.OnTapped() })
	onUI(a, func() {
		if a.home == nil {
			t.Error("answering the dialog jumped from Home to Settings")
		}
	})
}
