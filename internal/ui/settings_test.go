package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

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

	// language: German texts everywhere, saved
	onUI(a, func() {
		s, _ := find(a.win.Content(), func(s *widget.Select) bool { return true })
		s.SetSelected("Deutsch")
	})
	onUI(a, func() {
		if button(a.win.Content(), "Zurück") == nil {
			t.Error("no German texts after choosing Deutsch")
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
