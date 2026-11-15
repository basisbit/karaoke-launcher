package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/library"
	"github.com/basisbit/karaoke-launcher/internal/testfeed"
)

// themeApp opens the songs screen for a signed feed with a theme pack; trusted says whether the feed counts as one
// of launcher.json (with its key) or as one the user added.
func themeApp(t *testing.T, f *testfeed.Feed, trusted bool) (*App, *songs, string) {
	feeds := []library.Feed{{ID: "main", Address: f.Pub, Key: f.Key, Trusted: trusted}}
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en", feeds)
	a.o.Config.Content = map[string]config.Content{"theme": {Label: config.Text{"en": "Extras"},
		Dir: map[string]string{"linux": "{userdata}/themes"}, Extensions: []string{".ini", ".png"}}}
	a.o.Library.SetTargets(a.o.Config.Targets("linux", a.vars()))
	onUI(a, a.showSongs)
	var s *songs
	onUI(a, func() { s = a.songs })
	eventually(t, a, "packs listed", func() bool { return len(s.feeds) == 1 && s.feeds[0].checks["pop"] != nil })
	return a, s, filepath.Join(a.o.UserData, "themes")
}

func hasLabel(a *App, text string) bool {
	_, ok := find(a.win.Content(), func(l *widget.Label) bool { return l.Text == text })
	return ok
}

func TestSongsScreenThemes(t *testing.T) {
	f := testfeed.Signed(t)
	a, s, themes := themeApp(t, f, true)
	onUI(a, func() {
		if s.feeds[0].checks["themes"] == nil || !hasLabel(a, "Extras") {
			t.Error("theme pack of a trusted feed not shown under its label")
		}
	})
	onUI(a, func() { s.feeds[0].checks["pop"].SetChecked(true); s.feeds[0].checks["themes"].SetChecked(true) })
	onUI(a, func() { test.Tap(s.download) })
	eventually(t, a, "download done and list reloaded", func() bool {
		st := a.o.Library.Status()
		return !st.Running && st.Results["main"] != nil && len(s.feeds) == 1 && s.feeds[0].chosen["themes"]
	})
	if r := a.o.Library.Status().Results["main"]; len(r.Failed) != 0 {
		t.Fatalf("result: %+v", r)
	}
	for _, p := range []string{"Blue.ini", filepath.Join("Blue", "bg.png")} {
		if _, err := os.Stat(filepath.Join(themes, p)); err != nil {
			t.Fatalf("theme file %s missing: %v", p, err)
		}
	}
	eventually(t, a, "nothing left to download", func() bool { return strings.HasPrefix(s.needs.Text, "Still to download: 0 KB") })
	shot(t, a, "m7-songs-themes")

	// untick: a question that doesn't speak of songs, then the files go
	onUI(a, func() { s.feeds[0].checks["themes"].SetChecked(false) })
	eventually(t, a, "confirmation", func() bool { return len(a.win.Canvas().Overlays().List()) > 0 })
	var question bool
	onUI(a, func() {
		_, question = find(a.win.Canvas().Overlays().Top(), func(l *widget.Label) bool {
			return strings.HasPrefix(l.Text, "The files of “Themes” will be deleted")
		})
	})
	if !question {
		t.Error("the remove question for a theme pack is not the neutral text")
	}
	onUI(a, func() { button(a.win.Canvas().Overlays().Top(), "Remove").OnTapped() })
	eventually(t, a, "theme removed", func() bool {
		_, err := os.Stat(filepath.Join(themes, "Blue.ini"))
		return !a.o.Library.Status().Running && os.IsNotExist(err) && len(s.feeds) == 1 && !s.feeds[0].chosen["themes"]
	})
	if !a.o.Library.HasSongs() {
		t.Fatal("songs removed together with the theme pack")
	}
}

func TestSongsScreenThemesUntrusted(t *testing.T) {
	f := testfeed.Signed(t)
	a, s, _ := themeApp(t, f, false)
	onUI(a, func() {
		if s.feeds[0].checks["themes"] != nil || hasLabel(a, "Extras") {
			t.Error("theme pack shown for a feed the user added")
		}
	})
}
