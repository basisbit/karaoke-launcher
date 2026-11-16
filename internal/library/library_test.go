package library

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kfeed "github.com/basisbit/karaoke-feeds/tool"

	"github.com/basisbit/karaoke-launcher/internal/folders"
	"github.com/basisbit/karaoke-launcher/internal/testfeed"
)

const meta = `{"licenses": {"*": {"spdx": "CC-BY-4.0", "attribution": "Test Artist", "source": "https://example.org/src"}}}`

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
			txt := "#ARTIST:Tester\n#TITLE:" + n + "\n#BPM:300\n#GAP:0\n#MP3:" + n + ".mp3\n: 0 4 60 la\n: 8 4 62 la\nE\n"
			os.WriteFile(filepath.Join(d, n+".txt"), []byte(txt), 0o644)
			os.WriteFile(filepath.Join(d, n+".mp3"), []byte(strings.Repeat("dummy "+n, 500)), 0o644)
			os.WriteFile(filepath.Join(d, "feed-meta.json"), []byte(meta), 0o644)
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

// waitIdle waits until no download runs.
func waitIdle(t *testing.T, l *Library) Status {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if s := l.Status(); !s.Running {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("download did not end")
	return Status{}
}

func TestLibrary(t *testing.T) {
	one := makeFeed(t, map[string][]string{"Pop": {"a", "b"}, "Rock": {"c"}})
	two := makeFeed(t, map[string][]string{"Duets": {"d"}})
	userdata, root := t.TempDir(), filepath.Join(t.TempDir(), "library")
	feeds := []Feed{{ID: "one", Address: one}, {ID: "two", Address: two}, {ID: "never", Address: two}}
	var sawSecond atomic.Bool
	l, err := Open(userdata, root, feeds, func(s Status) {
		if s.FeedIndex == 2 && s.FeedCount == 3 && s.FeedID == "two" {
			sawSecond.Store(true)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(userdata, root, feeds, nil); !errors.Is(err, ErrRunning) {
		t.Fatalf("second instance: %v", err)
	}
	if l.HasSongs() {
		t.Fatal("empty library has songs")
	}
	ctx := context.Background()
	info, err := l.Inspect(ctx, "one")
	if err != nil || len(info.Packs) != 2 {
		t.Fatalf("Inspect: %+v %v", info, err)
	}
	if bytes, songs, err := l.Plan(ctx, "one", Choice{Packs: []string{"pop"}}); bytes <= 0 || songs != 2 || err != nil {
		t.Fatalf("Plan: %d %d %v", bytes, songs, err)
	}
	if _, err := l.Inspect(ctx, "nope"); err == nil {
		t.Fatal("unknown feed accepted")
	}

	if err := l.Start(map[string]Choice{"one": {Packs: []string{"pop"}}, "two": {Packs: []string{"duets"}}}); err != nil {
		t.Fatal(err)
	}
	if err := l.Start(nil); err != ErrBusy && l.Status().Running {
		t.Fatalf("second Start while running: %v", err)
	}
	s := waitIdle(t, l)
	if s.Err != nil || s.Paused || len(s.Results["one"].Added) != 2 || len(s.Results["two"].Added) != 1 || s.Results["never"] != nil {
		t.Fatalf("status: %+v", s)
	}
	if !sawSecond.Load() {
		t.Fatal("no status for the second feed")
	}
	if !l.HasSongs() {
		t.Fatal("no songs after download")
	}
	if m, _ := filepath.Glob(filepath.Join(root, "one", "*", "a.txt")); len(m) != 1 {
		t.Fatalf("a.txt not in <song folder>/one/<song>/: %v", m)
	}

	// again, with the stored choice: nothing to do
	l.Start(nil)
	s = waitIdle(t, l)
	if s.Err != nil || len(s.Results["one"].Added) != 0 || len(s.Results["two"].Added) != 0 {
		t.Fatalf("second run: %+v", s)
	}

	l.Close()
	l2, err := Open(userdata, root, feeds, nil)
	if err != nil {
		t.Fatalf("reopen after Close: %v", err)
	}
	l2.Close()
}

func TestPause(t *testing.T) {
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

	downloading := make(chan struct{}, 1)
	l, err := Open(t.TempDir(), t.TempDir(), []Feed{{ID: "main", Address: srv.URL + "/"}}, func(s Status) {
		if s.Progress.Phase == "downloading" {
			select {
			case downloading <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Start(map[string]Choice{"main": {Packs: []string{"pop"}}})
	select {
	case <-downloading:
	case <-time.After(10 * time.Second):
		t.Fatal("download did not start")
	}
	start := time.Now()
	l.Pause()
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Pause took %v", d)
	}
	s := l.Status()
	if s.Running || !s.Paused || s.Err != nil || l.HasSongs() {
		t.Fatalf("after Pause: %+v, songs %v", s, l.HasSongs())
	}

	stall.Store(false)
	l.Start(nil)
	s = waitIdle(t, l)
	if s.Err != nil || s.Paused || len(s.Results["main"].Added) != 2 || !l.HasSongs() {
		t.Fatalf("after resume: %+v", s)
	}
}

// TestExclusive: an install does not wait for a running download (that can take hours); the download is paused
// for it and continues afterwards.
func TestExclusive(t *testing.T) {
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
	downloading := make(chan struct{}, 1)
	l, err := Open(t.TempDir(), t.TempDir(), []Feed{{ID: "main", Address: srv.URL + "/"}}, func(s Status) {
		if s.Progress.Phase == "downloading" {
			select {
			case downloading <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Start(map[string]Choice{"main": {Packs: []string{"pop"}}})
	select {
	case <-downloading:
	case <-time.After(10 * time.Second):
		t.Fatal("download did not start")
	}
	ran := make(chan bool, 1)
	go l.Exclusive(func() { ran <- l.Status().Running })
	select {
	case running := <-ran:
		if running {
			t.Error("the download still ran during Exclusive")
		}
	case <-time.After(5 * time.Second):
		stall.Store(false)
		t.Fatal("Exclusive waited for the running download")
	}
	stall.Store(false)
	if s := waitIdle(t, l); s.Err != nil || len(s.Results["main"].Added) != 2 {
		t.Fatalf("the download did not continue after Exclusive: %+v", s)
	}
}

func TestAddedID(t *testing.T) {
	a, b := AddedID("https://example.org/feed/"), AddedID("https://example.org/feed/#ed25519=abc")
	if a != b || !strings.HasPrefix(a, "added-") || len(a) != len("added-")+8 || a == AddedID("https://example.org/other/") {
		t.Fatalf("%s %s", a, b)
	}
}

// TestSeeds: with the songs already in another folder (here: the feed's own source songs, as an "old
// installation"), everything is taken from there and nothing is downloaded.
func TestSeeds(t *testing.T) {
	pub := makeFeed(t, map[string][]string{"Pop": {"a", "b"}})
	l, err := Open(t.TempDir(), t.TempDir(), []Feed{{ID: "main", Address: pub}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.SetSeeds([]string{filepath.Join(filepath.Dir(pub), "songs")})
	if n, _, err := l.Plan(context.Background(), "main", Choice{Packs: []string{"pop"}}); err != nil || n != 0 {
		t.Fatalf("Plan with seeds: %d bytes, %v", n, err)
	}
	l.Start(map[string]Choice{"main": {Packs: []string{"pop"}}})
	s := waitIdle(t, l)
	r := s.Results["main"]
	if s.Err != nil || r == nil || len(r.Added) != 2 || r.BytesImported == 0 || r.BytesDownloaded != 0 || !l.HasSongs() {
		t.Fatalf("status %+v, result %+v", s, r)
	}
}

func TestThemes(t *testing.T) {
	f := testfeed.Signed(t)
	themes := filepath.Join(t.TempDir(), "themes")
	targets := map[string]kfeed.Target{"theme": {Dir: themes, Extensions: []string{".ini", ".png"}}}
	for _, tc := range []struct {
		name    string
		trusted bool
	}{{"trusted", true}, {"added by the user", false}} {
		t.Run(tc.name, func(t *testing.T) {
			os.RemoveAll(themes)
			userdata, root := t.TempDir(), filepath.Join(t.TempDir(), "library")
			l, err := Open(userdata, root, []Feed{{ID: "main", Address: f.Pub, Key: f.Key, Trusted: tc.trusted}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			l.SetTargets(targets)
			if err := l.Start(map[string]Choice{"main": {Packs: []string{"pop", "themes"}}}); err != nil {
				t.Fatal(err)
			}
			s := waitIdle(t, l)
			if s.Err != nil {
				t.Fatal(s.Err)
			}
			if !l.HasSongs() {
				t.Fatal("songs missing")
			}
			_, err = os.Stat(filepath.Join(themes, "Blue", "bg.png"))
			if got := err == nil; got != tc.trusted {
				t.Fatalf("theme files there: %v, want %v", got, tc.trusted)
			}
		})
	}
}

func TestTargetsAndReadmes(t *testing.T) {
	base := t.TempDir()
	root, themes := filepath.Join(base, "songs"), filepath.Join(base, "themes")
	l, err := Open(filepath.Join(base, "user"), root, []Feed{{ID: "main"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for dir, ok := range map[string]bool{root: false, base: false, filepath.Join(root, "main", "t"): false,
		filepath.Join(root, "themes"): true, themes: true} {
		l.SetTargets(map[string]kfeed.Target{"theme": {Dir: dir}})
		if _, got := l.Targets()["theme"]; got != ok {
			t.Errorf("target %s usable: %v, want %v", dir, got, ok)
		}
	}

	// README.md files go into the folders that exist; a folder marked for songs is no theme folder
	l.SetTargets(map[string]kfeed.Target{"theme": {Dir: themes}})
	os.MkdirAll(themes, 0o755)
	texts := map[string]string{folders.Songs: "# Songs", folders.Content("theme"): "# Themes"}
	l.SetReadmes(texts)
	if folders.Purpose(themes) != folders.Content("theme") {
		t.Fatal("theme folder not marked")
	}
	if _, err := os.Stat(root); err == nil {
		t.Fatal("song folder created just for its README.md")
	}
	os.MkdirAll(root, 0o755)
	l.SetReadmes(texts)
	if folders.Purpose(root) != folders.Songs {
		t.Fatal("song folder not marked")
	}
	l.SetTargets(map[string]kfeed.Target{"theme": {Dir: root}, "skin": {Dir: themes}})
	if len(l.Targets()) != 0 {
		t.Fatalf("marked folders used for another kind: %v", l.Targets())
	}
}

func TestHasSongsNotThemes(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "songs")
	l, err := Open(filepath.Join(base, "user"), root, []Feed{{ID: "main"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	themes, other := filepath.Join(root, "themes"), filepath.Join(root, "skins")
	l.SetTargets(map[string]kfeed.Target{"theme": {Dir: themes}})
	for _, dir := range []string{filepath.Join(themes, "Blue"), filepath.Join(other, "Red")} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644)
	}
	folders.Mark(other, folders.Content("skin"), "# Skins") // a content folder of an earlier configuration
	if l.HasSongs() {
		t.Fatal("a text file of a theme counts as a song")
	}
	os.MkdirAll(filepath.Join(root, "main", "Song"), 0o755)
	os.WriteFile(filepath.Join(root, "main", "Song", "song.txt"), []byte("#TITLE:x"), 0o644)
	if !l.HasSongs() {
		t.Fatal("song not seen")
	}
}
