package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"fyne.io/fyne/v2"

	"github.com/basisbit/karaoke-launcher/internal/library"
)

// humanBytes shows a size the way people read it: "480 MB", "2.3 GB".
func humanBytes(n int64) string {
	switch {
	case n >= 1e9:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 1, 64) + " GB"
	case n >= 1e6:
		return fmt.Sprintf("%.0f MB", float64(n)/1e6)
	default:
		return fmt.Sprintf("%.0f KB", float64(n)/1e3)
	}
}

// pick returns the text for the language code, else English, else the first one by key.
func pick(texts map[string]string, code string) string {
	if s, ok := texts[code]; ok {
		return s
	}
	if s, ok := texts["en"]; ok {
		return s
	}
	keys := make([]string, 0, len(texts))
	for k := range texts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return texts[keys[0]]
}

// isOffline guesses from an error whether the computer has no connection. The engine reports errors as text.
func isOffline(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{"dial tcp", "no such host", "connection refused", "network is unreachable",
		"i/o timeout", "connection reset", "tls handshake timeout", "server misbehaving", "unexpected eof",
		"broken pipe"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// statusText is the one-line status on the home screen.
func statusText(tr *Translator, s library.Status, gameRunning bool) string {
	if gameRunning {
		return tr.T("status.gamerunning")
	}
	if s.Running {
		var text string
		p := s.Progress
		switch p.Phase {
		case "downloading":
			text = tr.T("status.downloading", "done", strconv.Itoa(p.SongsDone), "total", strconv.Itoa(p.SongsTotal),
				"bytes", humanBytes(p.BytesDone), "totalbytes", humanBytes(p.BytesTotal))
		case "finishing":
			text = tr.T("status.finishing")
		default:
			text = tr.T("status.checking")
		}
		if s.FeedCount > 1 {
			text = tr.T("status.source", "index", strconv.Itoa(s.FeedIndex), "count", strconv.Itoa(s.FeedCount)) + " · " + text
		}
		return text
	}
	if s.Paused {
		return tr.T("status.paused")
	}
	if key := problemKey(s); key != "" {
		return tr.T(key)
	}
	return tr.T("status.uptodate")
}

// problemKey returns the text key for the problem of a finished run: "status.offline" when the feed or every failed
// file could not be reached, "status.problem" for other problems, "" when there was none.
func problemKey(s library.Status) string {
	var failed []string
	for _, r := range s.Results {
		if r != nil {
			failed = append(failed, r.Failed...)
		}
	}
	if s.Err == nil && len(failed) == 0 {
		return ""
	}
	offline := s.Err == nil || isOffline(s.Err)
	for _, f := range failed {
		offline = offline && isOffline(errors.New(f))
	}
	if offline {
		return "status.offline"
	}
	return "status.problem"
}

// newsLimit is the most news text that is read.
const newsLimit = 4096

// cleanNews makes downloaded news safe to show as plain text: valid UTF-8, no control characters except line breaks.
func cleanNews(b []byte) string {
	s := strings.ToValidUTF8(string(b), "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r): // also invisible format characters (bidi overrides)
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// fetchNews downloads the news text: at most newsLimit bytes, 10 seconds. Any problem gives an error; the caller
// then shows nothing.
func fetchNews(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New(resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, newsLimit))
	if err != nil {
		return "", err
	}
	return cleanNews(b), nil
}

// setVisible shows or hides o and reports whether that changed anything.
func setVisible(o fyne.CanvasObject, show bool) bool {
	if o.Visible() == show {
		return false
	}
	if show {
		o.Show()
	} else {
		o.Hide()
	}
	return true
}
