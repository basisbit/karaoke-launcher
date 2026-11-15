// Package library keeps the songs of all feeds in the song folder the user chose, with one sub-folder per feed
// (<song folder>/<feed id>/<song>/), and runs the downloads: one at a time, feeds one after another.
package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	kfeed "github.com/basisbit/karaoke-feeds/tool"

	"github.com/basisbit/karaoke-launcher/internal/folders"
)

// ErrRunning means another launcher instance uses the same per-user data folder.
var ErrRunning = errors.New("the launcher is already running")

// ErrBusy means a download is running; pause it first.
var ErrBusy = errors.New("a download is running")

// Feed is a feed of the library: from launcher.json, or added by the user.
type Feed struct {
	ID      string
	Address string
	Key     string
	Starter []string // packs ticked on first run
	// Trusted is true for a feed from launcher.json that has a key: only such a feed may install packs of other
	// kinds (themes), and only into the folders SetTargets gives (SPEC 5.6).
	Trusted bool
}

// AddedID returns the folder id for a feed the user added.
func AddedID(address string) string {
	address, _, _ = strings.Cut(address, "#")
	sum := sha256.Sum256([]byte(address))
	return "added-" + hex.EncodeToString(sum[:4])
}

// Choice is what to download from one feed. nil fields keep the stored choice.
type Choice struct {
	Packs   []string
	Exclude []string
}

// Status is the combined state of all downloads.
type Status struct {
	Running   bool
	FeedIndex int // 1-based number of the feed being worked on
	FeedCount int
	FeedID    string
	Progress  kfeed.Progress
	Err       error // the first problem of the last run; nil if it went well
	Paused    bool  // the last run was paused before it ended
	Results   map[string]*kfeed.Result
}

// Library is an open library folder.
type Library struct {
	root     string
	feeds    []Feed
	unlock   func()
	onChange func(Status)
	seeds    []string                // folders with songs the user has already (guarded by mu)
	targets  map[string]kfeed.Target // kind -> folder for packs of other kinds (trusted feeds only)
	readmes  map[string]string       // purpose (folders.Songs, folders.Content(kind)) -> text of its README.md

	engine sync.Mutex // the engine locks a feed folder: one engine call at a time

	mu     sync.Mutex
	st     Status
	cancel context.CancelFunc
	done   chan struct{}
}

// Open locks the per-user data folder against a second launcher and opens the library folder root.
// onChange (may be nil) is called from a background goroutine whenever the status changes.
func Open(userdata, root string, feeds []Feed, onChange func(Status)) (*Library, error) {
	if err := os.MkdirAll(userdata, 0o700); err != nil {
		return nil, err
	}
	unlock, err := lock(filepath.Join(userdata, "launcher.lock"))
	if err != nil {
		return nil, err
	}
	if onChange == nil {
		onChange = func(Status) {}
	}
	return &Library{root: root, feeds: feeds, unlock: unlock, onChange: onChange}, nil
}

// SetOnChange replaces the function that is called whenever the status changes (nil: none).
func (l *Library) SetOnChange(f func(Status)) {
	if f == nil {
		f = func(Status) {}
	}
	l.mu.Lock()
	l.onChange = f
	l.mu.Unlock()
}

// Close stops a running download and releases the lock.
func (l *Library) Close() {
	l.Pause()
	l.mu.Lock()
	unlock := l.unlock
	l.unlock = nil
	l.mu.Unlock()
	if unlock != nil { // a second Close (restart after an update, then the deferred one) does nothing
		unlock()
	}
}

// Root returns the library folder.
func (l *Library) Root() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.root
}

// Feeds returns the feeds of the library.
func (l *Library) Feeds() []Feed {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.feeds)
}

// SetRoot changes the library folder. The new folder starts empty; songs in the old one stay there.
func (l *Library) SetRoot(root string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.st.Running {
		return ErrBusy
	}
	l.root, l.st = root, Status{}
	return nil
}

// SetFeeds changes the feeds, for example after the user added one.
func (l *Library) SetFeeds(feeds []Feed) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.st.Running {
		return ErrBusy
	}
	l.feeds = slices.Clone(feeds)
	return nil
}

// Dir returns the songs folder of one feed.
func (l *Library) Dir(id string) string { return filepath.Join(l.Root(), id) }

func (l *Library) feed(id string) (Feed, error) {
	for _, f := range l.Feeds() {
		if f.ID == id {
			return f, nil
		}
	}
	return Feed{}, fmt.Errorf("no feed %q", id)
}

func (l *Library) options(f Feed, c Choice) kfeed.Options {
	l.mu.Lock()
	seeds := slices.Clone(l.seeds)
	l.mu.Unlock()
	o := kfeed.Options{Address: f.Address, Key: f.Key, Library: l.Dir(f.ID), Packs: c.Packs, Exclude: c.Exclude,
		Seeds: seeds}
	if f.Trusted && f.Key != "" {
		o.Targets = l.Targets()
	}
	return o
}

// Targets returns the folders for packs of other kinds that can be used now. A folder that is the song folder, holds
// it, overlaps the folder of a feed, or is marked for something else is left out (with its kind).
func (l *Library) Targets() map[string]kfeed.Target {
	l.mu.Lock()
	root, feeds, targets := l.root, slices.Clone(l.feeds), maps.Clone(l.targets)
	l.mu.Unlock()
	for kind, t := range targets {
		p := folders.Purpose(t.Dir)
		bad := folders.Within(root, t.Dir) || (p != "" && p != folders.Content(kind)) ||
			slices.ContainsFunc(feeds, func(f Feed) bool { return folders.Overlap(t.Dir, filepath.Join(root, f.ID)) })
		if bad {
			delete(targets, kind)
		}
	}
	return targets
}

// SetReadmes sets the texts of the README.md files in the folders the library fills (purpose -> text) and writes
// them into the folders that exist. Later syncs write them into new folders.
func (l *Library) SetReadmes(texts map[string]string) {
	l.mu.Lock()
	l.readmes = maps.Clone(texts)
	l.mu.Unlock()
	l.mark()
}

// mark writes the README.md files into the song folder and the content folders that exist. A folder marked for
// something else is left alone (Targets leaves such a content folder out).
func (l *Library) mark() {
	l.mu.Lock()
	root, texts := l.root, maps.Clone(l.readmes)
	l.mu.Unlock()
	if text := texts[folders.Songs]; text != "" {
		folders.Mark(root, folders.Songs, text)
	}
	for kind, t := range l.Targets() {
		if text := texts[folders.Content(kind)]; text != "" {
			folders.Mark(t.Dir, folders.Content(kind), text)
		}
	}
}

// SetTargets sets where packs of other kinds go (kind -> folder and allowed file types). Only trusted feeds use it.
func (l *Library) SetTargets(t map[string]kfeed.Target) {
	l.mu.Lock()
	l.targets = maps.Clone(t)
	l.mu.Unlock()
}

// SetSeeds sets the folders with songs the user has already: files with the same content are taken from there
// instead of being downloaded (the folders are not changed).
func (l *Library) SetSeeds(dirs []string) {
	l.mu.Lock()
	l.seeds = slices.Clone(dirs)
	l.mu.Unlock()
}

// Inspect fetches the description and packs of one feed.
func (l *Library) Inspect(ctx context.Context, id string) (*kfeed.Info, error) {
	f, err := l.feed(id)
	if err != nil {
		return nil, err
	}
	if l.Status().Running {
		return nil, ErrBusy
	}
	l.engine.Lock()
	defer l.engine.Unlock()
	return kfeed.Inspect(ctx, l.options(f, Choice{}))
}

// Plan reports the bytes to download and the songs that change for one feed with this choice.
func (l *Library) Plan(ctx context.Context, id string, c Choice) (int64, int, error) {
	f, err := l.feed(id)
	if err != nil {
		return 0, 0, err
	}
	if l.Status().Running {
		return 0, 0, ErrBusy
	}
	l.engine.Lock()
	defer l.engine.Unlock()
	return kfeed.Plan(ctx, l.options(f, c))
}

// Start downloads all feeds, one after another, in the background. A feed without a choice keeps its stored
// choice; a feed that was never chosen is skipped.
func (l *Library) Start(choices map[string]Choice) error {
	l.mu.Lock()
	if l.st.Running {
		l.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel, l.done = cancel, make(chan struct{})
	l.st = Status{Running: true, FeedCount: len(l.feeds), Results: map[string]*kfeed.Result{}}
	done, feeds := l.done, slices.Clone(l.feeds)
	l.mu.Unlock()
	l.changed()

	go func() {
		defer close(done)
		defer cancel()
		for i, f := range feeds {
			l.update(func(s *Status) { s.FeedIndex, s.FeedID, s.Progress = i+1, f.ID, kfeed.Progress{} })
			l.engine.Lock() // waits for an Inspect or Plan that started before
			res, err := kfeed.Sync(ctx, l.options(f, choices[f.ID]), func(p kfeed.Progress) {
				l.update(func(s *Status) { s.Progress = p })
			})
			l.engine.Unlock()
			if errors.Is(err, kfeed.ErrNoPacks) {
				continue // never chosen
			}
			l.update(func(s *Status) {
				if res != nil {
					s.Results[f.ID] = res
				}
				if err != nil && ctx.Err() == nil && s.Err == nil {
					s.Err = fmt.Errorf("%s: %w", f.ID, err)
				}
			})
			if ctx.Err() != nil {
				break
			}
		}
		l.mark() // new folders tell what they are for
		l.update(func(s *Status) { s.Running, s.Paused = false, ctx.Err() != nil })
	}()
	return nil
}

// Pause stops a running download soon and waits until it has stopped. A later Start continues.
func (l *Library) Pause() {
	l.mu.Lock()
	cancel, done := l.cancel, l.done
	l.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// Exclusive runs f while no feed is synced, inspected or planned, for example to install a program into a folder
// that a sync may also write to (themes inside the game's folder). A running sync finishes its current feed first,
// and the next feed waits until f returns.
func (l *Library) Exclusive(f func()) {
	l.engine.Lock()
	defer l.engine.Unlock()
	f()
}

// Status returns the current state.
func (l *Library) Status() Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.st
	s.Results = maps.Clone(l.st.Results)
	return s
}

func (l *Library) update(f func(*Status)) {
	l.mu.Lock()
	f(&l.st)
	l.mu.Unlock()
	l.changed()
}

func (l *Library) changed() {
	l.mu.Lock()
	f := l.onChange
	l.mu.Unlock()
	f(l.Status())
}

// HasSongs reports whether at least one song can be played: a song folder of any feed with a visible txt file.
func (l *Library) HasSongs() bool {
	l.mu.Lock()
	root, targets := l.root, maps.Clone(l.targets)
	l.mu.Unlock()
	feeds, _ := os.ReadDir(root)
	for _, f := range feeds {
		dir := filepath.Join(root, f.Name())
		if strings.HasPrefix(folders.Purpose(dir), folders.Content("")) ||
			slices.ContainsFunc(slices.Collect(maps.Values(targets)), func(t kfeed.Target) bool { return folders.Within(t.Dir, dir) }) {
			continue // a folder for themes or other content, not songs
		}
		songs, _ := os.ReadDir(filepath.Join(root, f.Name()))
		for _, s := range songs {
			if !s.IsDir() || strings.HasPrefix(s.Name(), ".") {
				continue
			}
			files, _ := os.ReadDir(filepath.Join(root, f.Name(), s.Name()))
			for _, file := range files {
				if strings.HasSuffix(strings.ToLower(file.Name()), ".txt") && file.Name() != "license.txt" {
					return true
				}
			}
		}
	}
	return false
}
