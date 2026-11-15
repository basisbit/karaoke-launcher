// Package config loads and checks launcher.json, the file a community ships next to the launcher.
// The file is trusted (it is installed together with the launcher), but it is still checked strictly, so that a
// mistake gives one clear message instead of strange behaviour later.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MaxSize is the largest launcher.json that is read.
const MaxSize = 1 << 20

var (
	idRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	packRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`) // catalog ids of the feed format
	colorRe  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	holderRe = regexp.MustCompile(`\{[^{}]*\}`)
)

// Config is a checked launcher.json (version 1). Unknown fields are ignored.
type Config struct {
	Config          int                 `json:"config"`
	ID              string              `json:"id"`
	Name            Text                `json:"name"` // "Karaoke" or {"en": "Karaoke", "ja": "カラオケ"}
	Look            Look                `json:"look"`
	Links           []Link              `json:"links"`
	News            *News               `json:"news"`
	Feeds           []Feed              `json:"feeds"`
	AllowOtherFeeds bool                `json:"allow_other_feeds"`
	Library         Library             `json:"library"`
	Start           map[string][]string `json:"start"` // "windows", "linux", "macos" -> arguments

	// Dir is the folder of the config file; the paths under Look are relative to it.
	Dir string `json:"-"`
}

// Text is a text the community writes: one string for all languages, or one per language code.
type Text map[string]string

// UnmarshalJSON accepts "text" (stored for every language, key "") and {"en": "…", "de": "…"}.
func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = Text{"": s}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return errors.New("must be a text or an object of texts per language")
	}
	*t = m
	return nil
}

// Get returns the text for a language code: that language, else the one text for all, else English, else the
// first by code.
func (t Text) Get(code string) string {
	for _, k := range []string{code, "", "en"} {
		if s, ok := t[k]; ok {
			return s
		}
	}
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return t[keys[0]]
}

// Look sets the appearance. Every field is optional.
type Look struct {
	Logo       string `json:"logo"`
	Background string `json:"background"`
	Font       string `json:"font"`
	Dark       bool   `json:"dark"`
	Colors     Colors `json:"colors"`
}

// Colors are "#rrggbb" or empty.
type Colors struct {
	Primary    string `json:"primary"`
	Background string `json:"background"`
	Text       string `json:"text"`
}

// Link is a button in the links row.
type Link struct {
	Label map[string]string `json:"label"` // language -> text
	URL   string            `json:"url"`
}

// News is a short plain text shown on the home screen.
type News struct {
	URL  string `json:"url"`
	More string `json:"more"`
}

// Feed is a song feed the community offers.
type Feed struct {
	ID      string   `json:"id"`
	Address string   `json:"address"`
	Key     string   `json:"key"`
	Starter []string `json:"starter"` // packs ticked on first run
}

// Library sets where songs go by default.
type Library struct {
	Default string `json:"default"`
}

// Vars are the values of the placeholders.
type Vars struct {
	Library  string // {library}: the song folder the user chose (songs are in <song folder>/<feed id>/)
	AppDir   string // {appdir}: the folder of the launcher executable
	UserData string // {userdata}: the per-user data folder
}

// Load reads and checks a launcher.json.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, MaxSize)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v", path, err)
	}
	abs, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	c.Dir = abs
	if c.Library.Default == "" {
		c.Library.Default = "{userdata}/songs"
	}
	if err := c.check(); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return &c, nil
}

func (c *Config) check() error {
	if c.Config != 1 {
		return fmt.Errorf(`"config" must be 1, not %d`, c.Config)
	}
	if !idRe.MatchString(c.ID) {
		return fmt.Errorf(`"id" %q must be 1 to 32 characters: a-z, 0-9 and "-", not starting with "-"`, c.ID)
	}
	if len(c.Name) == 0 {
		return errors.New(`"name" is missing`)
	}
	for code, s := range c.Name {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf(`"name" for %q is empty`, code)
		}
	}
	for field, p := range map[string]string{"logo": c.Look.Logo, "background": c.Look.Background, "font": c.Look.Font} {
		if p == "" {
			continue
		}
		if !localPath(p) {
			return fmt.Errorf(`look.%s %q must be a relative path inside the config folder`, field, p)
		}
		if _, err := os.Stat(c.Asset(p)); err != nil {
			return fmt.Errorf(`look.%s: %v`, field, err)
		}
	}
	for field, col := range map[string]string{"primary": c.Look.Colors.Primary, "background": c.Look.Colors.Background, "text": c.Look.Colors.Text} {
		if col != "" && !colorRe.MatchString(col) {
			return fmt.Errorf(`look.colors.%s %q must look like "#3b82f6"`, field, col)
		}
	}
	for i, l := range c.Links {
		if len(l.Label) == 0 {
			return fmt.Errorf("links[%d] has no label", i)
		}
		if err := CheckURL(l.URL); err != nil {
			return fmt.Errorf("links[%d].url: %v", i, err)
		}
	}
	if c.News != nil {
		if err := CheckURL(c.News.URL); err != nil {
			return fmt.Errorf("news.url: %v", err)
		}
		if c.News.More != "" {
			if err := CheckURL(c.News.More); err != nil {
				return fmt.Errorf("news.more: %v", err)
			}
		}
	}
	if len(c.Feeds) == 0 && !c.AllowOtherFeeds {
		return errors.New(`no "feeds", and "allow_other_feeds" is false: there would be nothing to download`)
	}
	seen := map[string]bool{}
	for i, f := range c.Feeds {
		if !idRe.MatchString(f.ID) {
			return fmt.Errorf("feeds[%d].id %q must be 1 to 32 characters: a-z, 0-9 and \"-\"", i, f.ID)
		}
		if seen[f.ID] {
			return fmt.Errorf("feeds[%d].id %q is used twice", i, f.ID)
		}
		seen[f.ID] = true
		if err := CheckURL(f.Address); err != nil {
			return fmt.Errorf("feeds[%d].address: %v", i, err)
		}
		if f.Key != "" && !strings.HasPrefix(f.Key, "ed25519:") {
			return fmt.Errorf(`feeds[%d].key must start with "ed25519:"`, i)
		}
		for _, p := range f.Starter {
			if !packRe.MatchString(p) {
				return fmt.Errorf("feeds[%d].starter: %q is not a valid pack id", i, p)
			}
		}
	}
	if _, err := Expand(c.Library.Default, Vars{AppDir: "a", UserData: "u"}, "appdir", "userdata"); err != nil {
		return fmt.Errorf("library.default: %v", err)
	}
	for system, args := range c.Start {
		if system != "windows" && system != "linux" && system != "macos" {
			continue // a later version may know more systems
		}
		if len(args) == 0 || args[0] == "" {
			return fmt.Errorf("start.%s: the first argument must be the program", system)
		}
		for _, a := range args {
			if _, err := Expand(a, Vars{Library: "l", AppDir: "a", UserData: "u"}); err != nil {
				return fmt.Errorf("start.%s: %v", system, err)
			}
		}
	}
	return nil
}

// localPath reports whether p is a relative path that stays inside its folder, on every system.
func localPath(p string) bool {
	if strings.ContainsAny(p, `\:`) || strings.HasPrefix(p, "/") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return false
		}
	}
	return filepath.IsLocal(filepath.FromSlash(p))
}

// CheckURL allows https, and http only for this computer (for development).
func CheckURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil {
		return fmt.Errorf("%q is not a web address", s)
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"):
		return nil
	}
	return fmt.Errorf("%q must start with https:// (http:// only for 127.0.0.1 or localhost)", s)
}

// Asset returns the full path of a path under Look.
func (c *Config) Asset(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Join(c.Dir, filepath.FromSlash(p))
}

// Expand replaces the placeholders in s. With allowed names given, only those placeholders may appear.
func Expand(s string, v Vars, allowed ...string) (string, error) {
	values := map[string]string{"library": v.Library, "appdir": v.AppDir, "userdata": v.UserData}
	var bad error
	out := holderRe.ReplaceAllStringFunc(s, func(h string) string {
		name := h[1 : len(h)-1]
		val, ok := values[name]
		if ok && len(allowed) > 0 {
			ok = false
			for _, a := range allowed {
				ok = ok || a == name
			}
		}
		if !ok && bad == nil {
			bad = fmt.Errorf("unknown placeholder %s in %q", h, s)
		}
		return val
	})
	return out, bad
}

// LibraryDefault returns the default library folder.
func (c *Config) LibraryDefault(v Vars) (string, error) {
	s, err := Expand(c.Library.Default, v, "appdir", "userdata")
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.FromSlash(s)), nil
}

// UserDataDir returns the per-user data folder: the system's config folder plus the id.
func (c *Config) UserDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, c.ID), nil
}

// StartArgs returns the start command for a Go system name (runtime.GOOS), with placeholders replaced.
// ok is false when the config has no command for this system.
func (c *Config) StartArgs(goos string, v Vars) (args []string, ok bool, err error) {
	system := map[string]string{"windows": "windows", "linux": "linux", "darwin": "macos"}[goos]
	raw, ok := c.Start[system]
	if !ok || system == "" {
		return nil, false, nil
	}
	for _, a := range raw {
		e, err := Expand(a, v)
		if err != nil {
			return nil, true, err
		}
		args = append(args, e)
	}
	return args, true, nil
}
