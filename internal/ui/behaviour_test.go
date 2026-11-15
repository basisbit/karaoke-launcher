package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"fyne.io/fyne/v2/widget"

	"github.com/basisbit/karaoke-launcher/internal/library"
)

func TestStartChecksAndUpdates(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a"}})
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en",
		[]library.Feed{{ID: "main", Address: pub}})
	a.o.Library.Start(map[string]library.Choice{"main": {Packs: []string{"pop"}}})
	eventually(t, a, "first download", func() bool { return a.o.Library.HasSongs() && !a.o.Library.Status().Running })

	onUI(a, a.Start)
	eventually(t, a, "feed checked and update run", func() bool {
		a.infoMu.Lock()
		checked := a.infos["main"] != nil
		a.infoMu.Unlock()
		st := a.o.Library.Status()
		r := st.Results["main"]
		return checked && !st.Running && r != nil && len(r.Added) == 0 && st.Err == nil
	})
	onUI(a, func() {
		if a.home.status.Text != "Everything is up to date" || a.home.details.Visible() {
			t.Errorf("status %q, details %v", a.home.status.Text, a.home.details.Visible())
		}
	})
}

func TestCloseDuringDownload(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a", "b"}})
	var stall atomic.Bool
	stall.Store(true)
	files := http.FileServer(http.Dir(pub))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stall.Load() && strings.HasPrefix(r.URL.Path, "/blobs/") {
			w.WriteHeader(200)
			io.WriteString(w, "dummy")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer srv.Close()
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en",
		[]library.Feed{{ID: "main", Address: srv.URL + "/"}})
	a.o.Library.Start(map[string]library.Choice{"main": {Packs: []string{"pop"}}})
	eventually(t, a, "downloading", func() bool { return a.o.Library.Status().Progress.Phase == "downloading" })
	eventually(t, a, "home shows progress", func() bool { return a.home.progress.Visible() || a.home.status.Text != "" })
	shot(t, a, "m3-home-downloading")

	onUI(a, a.onClose)
	eventually(t, a, "close question", func() bool { return len(a.win.Canvas().Overlays().List()) > 0 })
	shot(t, a, "m3-close-question")
	onUI(a, func() { button(a.win.Canvas().Overlays().Top(), "Stop and close").OnTapped() })
	eventually(t, a, "paused", func() bool {
		st := a.o.Library.Status()
		return !st.Running && st.Paused
	})
	stall.Store(false)
}

func TestProblemDetails(t *testing.T) {
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en",
		[]library.Feed{{ID: "gone", Address: "http://127.0.0.1:1/"}})
	os.MkdirAll(a.o.Library.Dir("gone"), 0o755)
	a.o.Library.Start(map[string]library.Choice{"gone": {Packs: []string{"pop"}}})
	eventually(t, a, "failed", func() bool { st := a.o.Library.Status(); return !st.Running && st.Err != nil })
	eventually(t, a, "details button", func() bool {
		return a.home.details.Visible() && strings.HasPrefix(a.home.status.Text, "No connection")
	})
	onUI(a, a.home.details.OnTapped)
	eventually(t, a, "details dialog", func() bool { return len(a.win.Canvas().Overlays().List()) > 0 })
	shot(t, a, "m3-home-problem")
}

// TestPauseResume: Pause on the songs screen shows "paused" and a Resume button; Resume continues the download.
func TestPauseResume(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a", "b"}})
	var stall atomic.Bool
	stall.Store(true)
	files := http.FileServer(http.Dir(pub))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stall.Load() && strings.HasPrefix(r.URL.Path, "/blobs/") {
			w.WriteHeader(200)
			io.WriteString(w, "dummy")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer srv.Close()
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en",
		[]library.Feed{{ID: "main", Address: srv.URL + "/", Starter: []string{"pop"}}})
	onUI(a, a.showSongs)
	var s *songs
	onUI(a, func() { s = a.songs })
	eventually(t, a, "needs line", func() bool { return strings.HasPrefix(s.needs.Text, "Still to download: ") })
	onUI(a, s.download.OnTapped)
	eventually(t, a, "downloading", func() bool { return a.o.Library.Status().Progress.Phase == "downloading" })
	eventually(t, a, "pause enabled", func() bool { return !s.pause.Disabled() })
	onUI(a, s.pause.OnTapped)
	eventually(t, a, "paused", func() bool { return a.o.Library.Status().Paused })
	eventually(t, a, "paused shown", func() bool {
		return s.status.Text == "Download paused" && s.download.Text == "Resume" && !s.download.Disabled() &&
			s.pause.Disabled() && s.progress.Visible()
	})
	// the list reloads after the download stopped; the paused state must stay visible
	eventually(t, a, "still paused after reload", func() bool {
		return len(s.feeds) == 1 && s.feeds[0].info != nil && s.status.Text == "Download paused" && s.download.Text == "Resume"
	})
	shot(t, a, "m3-songs-paused")
	stall.Store(false)
	onUI(a, s.download.OnTapped)
	eventually(t, a, "finished", func() bool {
		st := a.o.Library.Status()
		return !st.Running && !st.Paused && a.o.Library.HasSongs()
	})
	eventually(t, a, "update label", func() bool { return s.download.Text == "Update" })
}

// TestPlayMissingGame: a start command that doesn't exist gives a clear sentence, not "Something went wrong".
func TestPlayMissingGame(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a"}})
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "dark", "launcher.json"), "en",
		[]library.Feed{{ID: "main", Address: pub}})
	a.o.Library.Start(map[string]library.Choice{"main": {Packs: []string{"pop"}}})
	eventually(t, a, "songs there", func() bool { return a.o.Library.HasSongs() && !a.o.Library.Status().Running })
	onUI(a, a.showHome)
	onUI(a, a.home.onPlay) // examples/dark starts "karaoke-game", which is not installed here
	eventually(t, a, "error shown", func() bool {
		_, ok := find(a.win.Canvas().Overlays().Top(), func(l *widget.Label) bool {
			return l.Text == "The game could not be started. Is it installed?"
		})
		return ok
	})
	onUI(a, func() {
		if a.gameRunning {
			t.Error("game marked as running after a failed start")
		}
	})
}
