// Package settings keeps the per-user choices in <userdata>/settings.json.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// FileName is the name of the settings file in the per-user data folder.
const FileName = "settings.json"

const maxSize = 1 << 20

// Feed is a feed the user added.
type Feed struct {
	Address string `json:"address"`
	Key     string `json:"key,omitempty"`
}

// Settings are the user's choices.
type Settings struct {
	Library  string `json:"library"`            // library folder
	Language string `json:"language,omitempty"` // "" = the system's language
	Feeds    []Feed `json:"feeds,omitempty"`    // feeds the user added
	// Exclude lists licence properties the user doesn't want ("nc", "nd", "sa"); applied to every feed.
	Exclude []string `json:"exclude,omitempty"`
	// Import lists folders with songs the user has already (an older installation); they are used instead of
	// downloading the same files again.
	Import []string `json:"import,omitempty"`

	path     string
	firstRun bool
}

// Load reads the settings from dir. Without a settings file it returns the defaults, and FirstRun reports true.
func Load(dir, defaultLibrary string) (*Settings, error) {
	path := filepath.Join(dir, FileName)
	s := &Settings{path: path}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		s.Library, s.firstRun = defaultLibrary, true
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, fmt.Errorf("%s is too large", path)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s is damaged: %v", path, err)
	}
	if s.Library == "" {
		s.Library = defaultLibrary
	}
	return s, nil
}

// FirstRun reports whether there was no settings file yet.
func (s *Settings) FirstRun() bool { return s.firstRun }

// Save writes the settings atomically: a crash leaves either the old or the new file. Only the user may read it,
// because a feed address can contain a personal access token.
func (s *Settings) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, FileName+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	// Windows has no Unix file modes; there the user's profile folder already keeps others out.
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	s.firstRun = false
	return nil
}
