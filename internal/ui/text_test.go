package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kfeed "github.com/basisbit/karaoke-feeds/tool"

	"github.com/basisbit/karaoke-launcher/internal/library"
)

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{999: "1 KB", 480e6: "480 MB", 2_345_000_000: "2.3 GB", 18e9: "18.0 GB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("%d: %q, want %q", n, got, want)
		}
	}
}

func TestPick(t *testing.T) {
	m := map[string]string{"en": "Website", "de": "Webseite", "ab": "x"}
	if pick(m, "de") != "Webseite" || pick(m, "fr") != "Website" || pick(map[string]string{"b": "B", "a": "A"}, "fr") != "A" || pick(nil, "de") != "" {
		t.Fatal("pick")
	}
}

func TestStatusText(t *testing.T) {
	tr := NewTranslator("en")
	cases := []struct {
		s    library.Status
		game bool
		want string
	}{
		{library.Status{}, false, "Everything is up to date"},
		{library.Status{Running: true}, false, "Looking for new songs"},
		{library.Status{Running: true, FeedCount: 1, Progress: kfeed.Progress{Phase: "downloading", SongsDone: 3, SongsTotal: 12,
			BytesDone: 120e6, BytesTotal: 1.4e9}}, false, "Downloading songs: 3 of 12, 120 MB of 1.4 GB"},
		{library.Status{Running: true, FeedIndex: 2, FeedCount: 3, Progress: kfeed.Progress{Phase: "finishing"}}, false, "Song source 2 of 3 · Finishing"},
		{library.Status{Paused: true}, false, "Download paused"},
		{library.Status{Err: errors.New(`main: no usable feed.json: https://x/: dial tcp: lookup x: no such host`)}, false, "No connection"},
		{library.Status{Err: errors.New("main: signature missing")}, false, "Some songs could not"},
		{library.Status{Results: map[string]*kfeed.Result{"a": {Failed: []string{"x"}}}}, false, "Some songs could not"},
		{library.Status{Results: map[string]*kfeed.Result{"a": {Failed: []string{
			"/lib/a/a.mp3: dial tcp 127.0.0.1:8080: connect: connection refused (after 3 attempts per location)",
			"/lib/b/b.mp3: read tcp 127.0.0.1:5→127.0.0.1:8080: read: connection reset by peer",
			"/lib/c/c.mp4: http://127.0.0.1:8080/: download broke off after 10 of 99 bytes: unexpected EOF"}}}}, false, "No connection"},
		{library.Status{Results: map[string]*kfeed.Result{"a": {Failed: []string{
			"/lib/a/a.mp3: dial tcp: connection refused", "/lib/b/b.mp3: content does not match its hash"}}}}, false, "Some songs could not"},
		{library.Status{Running: true}, true, "The game is running"},
	}
	for _, c := range cases {
		if got := statusText(tr, c.s, c.game); !strings.HasPrefix(got, c.want) {
			t.Errorf("%+v: %q, want %q…", c.s, got, c.want)
		}
	}
}

func TestCleanNews(t *testing.T) {
	in := "  Party on Friday!\r\n\tBring\x00 friends\x1b[31m \u202eevil\u202c\nbad utf8 \xff end  "
	want := "Party on Friday!\n Bring friends[31m evil\nbad utf8  end"
	if got := cleanNews([]byte(in)); got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestFetchNews(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Write([]byte(strings.Repeat("a", 10000)))
		case "/news":
			w.Write([]byte("Hello\x07 world"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if s, err := fetchNews(ctx, srv.URL+"/news"); s != "Hello world" || err != nil {
		t.Fatalf("news: %q %v", s, err)
	}
	if s, _ := fetchNews(ctx, srv.URL+"/big"); len(s) != newsLimit {
		t.Fatalf("big: %d bytes", len(s))
	}
	if _, err := fetchNews(ctx, srv.URL+"/missing"); err == nil {
		t.Fatal("404 accepted")
	}
}
