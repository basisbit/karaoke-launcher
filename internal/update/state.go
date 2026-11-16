// Package update installs and updates the programs of a bundle (the launcher itself and, for example, the game)
// from a signed app.json that the community publishes. See docs/updates.md.
package update

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// State is what is installed of a component: the file <dir>/.component-<id>.json.
type State struct {
	Component string   `json:"component"`
	Version   string   `json:"version"`
	Files     []string `json:"files"` // paths with "/"; files that match the component's keep patterns are not listed
}

// ready is <dir>/.update-<id>/ready.json: an unpacked update that waits to be applied. It is written last.
type ready struct {
	Version string   `json:"version"`
	From    string   `json:"from"`   // the installed version it was staged for ("" = none installed)
	SHA256  string   `json:"sha256"` // of the archive it was unpacked from
	Files   []string `json:"files"`
}

const maxStateSize = 64 << 20 // a list of 100,000 paths of up to a few hundred bytes

func statePath(dir, id string) string  { return filepath.Join(dir, ".component-"+id+".json") }
func stagingDir(dir, id string) string { return filepath.Join(dir, ".update-"+id) }
func readyPath(dir, id string) string  { return filepath.Join(stagingDir(dir, id), "ready.json") }
func oldListPath(dir string) string    { return filepath.Join(dir, ".update-old.txt") }

// Installed returns the state of a component, or nil when it has none.
func Installed(dir, id string) (*State, error) {
	var s State
	if err := readJSON(statePath(dir, id), &s); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// WriteState writes the state of a component atomically.
func WriteState(dir string, s State) error {
	return writeJSON(statePath(dir, s.Component), s)
}

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || fi.Size() > maxStateSize {
		return errors.Join(err, errors.New(path+" is too large"))
	}
	if err := json.NewDecoder(f).Decode(v); err != nil {
		return errors.New(path + " is damaged: " + err.Error())
	}
	return nil
}

// writeJSON writes a file atomically: a crash leaves either the old or the new file.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	_, err = tmp.Write(append(data, '\n'))
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// safeRel reports whether p (with "/") is a relative path that stays inside its folder on every system. Lists
// read from disk are checked with it before a file is moved or deleted.
func safeRel(p string) bool {
	if p == "" || strings.ContainsAny(p, `\:`) || strings.HasPrefix(p, "/") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return filepath.IsLocal(filepath.FromSlash(p))
}
