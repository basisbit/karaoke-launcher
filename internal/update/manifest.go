package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	kfeed "github.com/basisbit/karaoke-feeds/tool"
	"github.com/basisbit/karaoke-launcher/internal/config"
)

// Limits of the check (docs/updates.md, rule 1).
const (
	maxManifest  = 1 << 20
	maxSignature = 1 << 10
	checkTimeout = 15 * time.Second
)

var hashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Manifest is app.json.
type Manifest struct {
	App        int                  `json:"app"`
	ID         string               `json:"id"`
	Sequence   int64                `json:"sequence"`
	Components map[string]Component `json:"components"`
}

// Component is one program in app.json.
type Component struct {
	Version string          `json:"version"`
	Files   map[string]File `json:"files"` // "<GOOS>-<GOARCH>" -> archive
}

// File is the archive of a component for one system.
type File struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Strip  int    `json:"strip"`
}

// Target is a component this launcher installs: its id, its folder and its keep patterns.
type Target struct {
	ID   string
	Dir  string
	Keep []string
}

// Updater checks for and installs program updates.
type Updater struct {
	URL      string // address of app.json
	Key      string // public key that signs app.json
	ID       string // id of launcher.json; app.json must have the same
	UserData string // the launcher's per-user folder (update.json, downloads)
	Targets  []Target
	Platform string // "<GOOS>-<GOARCH>"; empty: this system
}

// Update is a component that can be installed now.
type Update struct {
	Target
	Version string
	File    File // URL resolved against the address of app.json
}

// Result is what a check found.
type Result struct {
	Updates []Update
	Notice  bool // the launcher has a new version that can't be installed here (for example macOS)
}

func (u *Updater) platform() string {
	if u.Platform != "" {
		return u.Platform
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

// Check fetches app.json and its signature and returns what can be updated. Any error means: nothing changes, and
// the user is not told (docs/updates.md, rule 3).
func (u *Updater) Check(ctx context.Context) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	data, err := get(ctx, u.URL, maxManifest)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	sigURL := u.URL[:strings.LastIndex(u.URL, "/")+1] + "sig/" + hex.EncodeToString(sum[:]) + ".sig"
	sig, err := get(ctx, sigURL, maxSignature)
	if err != nil {
		return nil, fmt.Errorf("signature: %v", err)
	}
	if !kfeed.Verify([]string{u.Key}, data, string(sig)) {
		return nil, errors.New("app.json is not signed with the key in launcher.json")
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("app.json: %v", err)
	}
	if m.App != 1 {
		return nil, fmt.Errorf(`app.json: "app" is %d, this launcher knows 1`, m.App)
	}
	if m.ID != u.ID {
		return nil, fmt.Errorf("app.json is for %q, not %q", m.ID, u.ID)
	}
	if err := u.storeSequence(m.Sequence); err != nil {
		return nil, err
	}
	res := &Result{}
	for _, t := range u.Targets {
		c, ok := m.Components[t.ID]
		if !ok {
			continue
		}
		st, err := Installed(t.Dir, t.ID)
		if err != nil {
			continue
		}
		f, ok := c.Files[u.platform()]
		updatable := ok && writable(t.Dir)
		if ok {
			f, err = u.resolve(f)
			updatable = updatable && err == nil
		}
		changed := st != nil && st.Version != c.Version
		if t.ID == config.Launcher && changed && !updatable {
			res.Notice = true
		}
		if !updatable || !(changed || (st == nil && t.ID != config.Launcher && empty(t.Dir))) {
			continue // a folder with files but no state is not ours; the launcher without state is a developer run
		}
		res.Updates = append(res.Updates, Update{Target: t, Version: c.Version, File: f})
	}
	return res, nil
}

// resolve checks a file entry and makes its address absolute.
func (u *Updater) resolve(f File) (File, error) {
	base, err := url.Parse(u.URL)
	if err != nil {
		return f, err
	}
	ref, err := url.Parse(f.URL)
	if err != nil {
		return f, err
	}
	f.URL = base.ResolveReference(ref).String()
	switch {
	case config.CheckURL(f.URL) != nil:
		return f, config.CheckURL(f.URL)
	case Ending(ref.Path) == "":
		return f, fmt.Errorf("%s is not a .zip, .tar.gz or .tgz file", ref.Path)
	case !hashRe.MatchString(f.SHA256) || f.Size <= 0 || f.Strip < 0:
		return f, errors.New("file entry needs sha256, size and strip >= 0")
	}
	return f, nil
}

// storeSequence refuses an app.json older than the newest seen (rule 2) and remembers the new one.
func (u *Updater) storeSequence(seq int64) error {
	path := filepath.Join(u.UserData, "update.json")
	var st struct {
		Sequence int64 `json:"sequence"`
	}
	if err := readJSON(path, &st); err != nil && !os.IsNotExist(err) {
		return err
	}
	if seq < st.Sequence {
		return fmt.Errorf("app.json has sequence %d, older than %d", seq, st.Sequence)
	}
	if seq == st.Sequence {
		return nil
	}
	st.Sequence = seq
	if err := os.MkdirAll(u.UserData, 0o700); err != nil {
		return err
	}
	return writeJSON(path, st)
}

// get downloads a small file; more than max bytes is an error.
func get(ctx context.Context, addr string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", req.URL.Redacted(), resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err == nil && int64(len(data)) > max {
		err = fmt.Errorf("%s is larger than %d bytes", req.URL.Redacted(), max)
	}
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), err
}

var client = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 || (via[0].URL.Scheme == "https" && req.URL.Scheme != "https") {
		return errors.New("too many redirects, or redirect from https to http")
	}
	return nil
}}

// writable reports whether files can be created in dir, or in the folder that would hold it when it is missing.
func writable(dir string) bool {
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	f.Close()
	return os.Remove(f.Name()) == nil
}

// empty reports whether dir is missing or has no entries.
func empty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return (err != nil && os.IsNotExist(err)) || (err == nil && len(entries) == 0)
}
