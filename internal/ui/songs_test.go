package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	kfeed "github.com/basisbit/karaoke-feeds/tool"

	"github.com/basisbit/karaoke-launcher/internal/library"
)

const testMeta = `{"licenses": {"*": {"spdx": "CC-BY-4.0", "attribution": "Test Artist", "source": "https://example.org/src"}}}`

// makeFeed builds an unsigned feed with kfeed build. songs maps a catalog folder to song names.
func makeFeed(t *testing.T, songs map[string][]string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "feed-config.json"),
		[]byte(`{"name":"Test Feed","operator":{"name":"Tester","contact":"mailto:t@example.com"}}`), 0o644)
	for cat, names := range songs {
		for _, n := range names {
			d := filepath.Join(dir, "songs", cat, n)
			os.MkdirAll(d, 0o755)
			txt := "#ARTIST:Tester\n#TITLE:" + n + "\n#BPM:300\n#GAP:0\n#MP3:" + n + ".mp3\n: 0 4 60 la\nE\n"
			os.WriteFile(filepath.Join(d, n+".txt"), []byte(txt), 0o644)
			os.WriteFile(filepath.Join(d, n+".mp3"), []byte(strings.Repeat("dummy "+n, 20000)), 0o644)
			os.WriteFile(filepath.Join(d, "feed-meta.json"), []byte(testMeta), 0o644)
		}
	}
	old := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	code := kfeed.Run([]string{"kfeed", "build", dir}, "test")
	os.Stdout.Close()
	os.Stdout = old
	if code != 0 {
		t.Fatalf("kfeed build: exit %d", code)
	}
	return filepath.Join(dir, "public")
}

// eventually waits until cond (run on the Fyne goroutine) is true.
func eventually(t *testing.T, a *App, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ok := false
		onUI(a, func() { ok = cond() })
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func TestSongsScreen(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a", "b"}, "Rock": {"c"}})
	feeds := []library.Feed{{ID: "main", Address: pub, Starter: []string{"pop"}}}
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en", feeds)
	onUI(a, a.showSongs)
	var s *songs
	onUI(a, func() { s = a.songs })
	eventually(t, a, "packs listed", func() bool { return len(s.feeds) == 1 && s.feeds[0].checks["pop"] != nil })
	onUI(a, func() {
		if !s.feeds[0].checks["pop"].Checked || s.feeds[0].checks["rock"].Checked {
			t.Error("first run: the starter pack pop must be ticked, rock not")
		}
	})
	eventually(t, a, "needs line", func() bool { return strings.HasPrefix(s.needs.Text, "Still to download: ") })
	var before string
	onUI(a, func() { before = s.needs.Text; s.feeds[0].checks["rock"].SetChecked(true) })
	eventually(t, a, "needs line changes", func() bool { return strings.HasPrefix(s.needs.Text, "Still to download: ") && s.needs.Text != before })
	onUI(a, func() { s.feeds[0].checks["rock"].SetChecked(false) })
	shot(t, a, "m3-songs-light")

	onUI(a, func() { test.Tap(s.download) })
	eventually(t, a, "download done", func() bool {
		st := a.o.Library.Status()
		return !st.Running && st.Results["main"] != nil
	})
	if r := a.o.Library.Status().Results["main"]; len(r.Added) != 2 || len(r.Failed) != 0 {
		t.Fatalf("result: %+v", r)
	}
	eventually(t, a, "list reloaded with pop chosen", func() bool {
		return len(s.feeds) == 1 && s.feeds[0].chosen["pop"] && s.download.Text == "Update"
	})

	onUI(a, a.showHome)
	onUI(a, func() {
		if a.home.play.Text != "Play" || a.home.play.Disabled() {
			t.Errorf("Play after download: %q", a.home.play.Text)
		}
	})

	// unticking a pack in the library asks first; Remove deletes the songs and the pack stays unticked
	onUI(a, a.showSongs)
	onUI(a, func() { s = a.songs })
	eventually(t, a, "packs listed again", func() bool { return len(s.feeds) == 1 && s.feeds[0].chosen["pop"] })
	onUI(a, func() { s.feeds[0].checks["pop"].SetChecked(false) })
	eventually(t, a, "confirmation", func() bool { return len(a.win.Canvas().Overlays().List()) > 0 })
	shot(t, a, "m3-songs-remove")
	onUI(a, func() { button(a.win.Canvas().Overlays().Top(), "Remove").OnTapped() })
	eventually(t, a, "removed and reloaded", func() bool {
		return !a.o.Library.Status().Running && len(s.feeds) == 1 && s.feeds[0].info.Subscribed && strings.HasPrefix(s.needs.Text, "Still to download: 0 KB") &&
			!s.feeds[0].chosen["pop"] && !s.feeds[0].checks["pop"].Checked
	})
	if a.o.Library.HasSongs() {
		t.Fatal("songs still there after Remove")
	}
	// the screen built again later keeps it unticked (no starter pack after "remove all")
	onUI(a, a.showSongs)
	onUI(a, func() { s = a.songs })
	eventually(t, a, "reloaded", func() bool { return len(s.feeds) == 1 })
	onUI(a, func() {
		if s.feeds[0].checks["pop"].Checked {
			t.Error("starter pack ticked again after removing all packs")
		}
	})
}

// TestSongsKeepsTicks: a reload of the list (after a download ended) keeps the ticks the user has not applied yet.
func TestSongsKeepsTicks(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a"}, "Rock": {"c"}})
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en", []library.Feed{{ID: "main", Address: pub}})
	onUI(a, a.showSongs)
	var s *songs
	onUI(a, func() { s = a.songs })
	eventually(t, a, "packs listed", func() bool { return len(s.feeds) == 1 && s.feeds[0].checks["rock"] != nil })
	var old *feedView
	onUI(a, func() { old = s.feeds[0]; s.feeds[0].checks["rock"].SetChecked(true) })
	onUI(a, s.reload) // what the end of a download does
	eventually(t, a, "list reloaded", func() bool { return len(s.feeds) == 1 && s.feeds[0] != old })
	onUI(a, func() {
		if !s.feeds[0].checks["rock"].Checked {
			t.Error("the reload wiped a tick the user had not applied yet")
		}
	})
}

// TestSongsPlanError: when the estimate fails, Download is not left disabled by an earlier "not enough space".
func TestSongsPlanError(t *testing.T) {
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en", nil)
	onUI(a, a.showSongs)
	onUI(a, func() {
		s := a.songs
		s.showPlan(1<<50, 1, nil)
		if !s.download.Disabled() {
			t.Fatal("Download enabled although nothing fits")
		}
		s.showPlan(0, 0, errors.New("estimate failed"))
		if s.download.Disabled() {
			t.Error("Download stays disabled after a failed estimate")
		}
	})
}

// TestSongsChecksWhileRunning: packs can't be changed while a download runs (pause first).
func TestSongsChecksWhileRunning(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a"}})
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en", []library.Feed{{ID: "main", Address: pub}})
	onUI(a, a.showSongs)
	var s *songs
	onUI(a, func() { s = a.songs })
	eventually(t, a, "packs listed", func() bool { return len(s.feeds) == 1 && s.feeds[0].checks["pop"] != nil })
	var old *feedView
	onUI(a, func() {
		old = s.feeds[0]
		s.refresh(library.Status{Running: true})
		if !s.feeds[0].checks["pop"].Disabled() {
			t.Error("a pack can be changed during a download")
		}
		s.refresh(library.Status{Paused: true})
		if s.feeds[0].checks["pop"].Disabled() {
			t.Error("a pack can't be changed while the download is paused")
		}
	})
	// the end of the "download" reloads the list in the background: wait for it before the folders are removed
	eventually(t, a, "list reloaded", func() bool { return len(s.feeds) == 1 && s.feeds[0] != old })
}

// TestSongsRemoveOnly: confirming the removal of one pack removes it and does not download other packs that are
// ticked but not applied (that needs Download, with its free-space check).
func TestSongsRemoveOnly(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a"}, "Rock": {"c"}})
	a := testAppFeeds(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en",
		[]library.Feed{{ID: "main", Address: pub, Starter: []string{"pop"}}})
	onUI(a, a.showSongs)
	var s *songs
	onUI(a, func() { s = a.songs })
	eventually(t, a, "packs listed", func() bool { return len(s.feeds) == 1 && s.feeds[0].checks["pop"] != nil })
	onUI(a, func() { test.Tap(s.download) })
	eventually(t, a, "pop downloaded", func() bool {
		return !a.o.Library.Status().Running && len(s.feeds) == 1 && s.feeds[0].chosen["pop"]
	})
	onUI(a, func() { s.feeds[0].checks["rock"].SetChecked(true); s.feeds[0].checks["pop"].SetChecked(false) })
	eventually(t, a, "confirmation", func() bool { return len(a.win.Canvas().Overlays().List()) > 0 })
	onUI(a, func() { button(a.win.Canvas().Overlays().Top(), "Remove").OnTapped() })
	eventually(t, a, "pop removed", func() bool {
		return !a.o.Library.Status().Running && len(s.feeds) == 1 && !s.feeds[0].chosen["pop"] && s.feeds[0].checks["pop"] != nil
	})
	onUI(a, func() {
		if s.feeds[0].chosen["rock"] {
			t.Error("rock was downloaded by confirming the removal of pop")
		}
		if !s.feeds[0].checks["rock"].Checked {
			t.Error("the tick on rock was lost")
		}
	})
}
