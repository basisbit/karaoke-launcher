// Package config loads and checks launcher.json, the file a community ships next to the launcher.
// The file is trusted (it is installed together with the launcher), but it is still checked strictly, so that a
// mistake gives one clear message instead of strange behaviour later.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	kfeed "github.com/basisbit/karaoke-feeds/tool"
)

// MaxSize is the largest launcher.json that is read.
const MaxSize = 1 << 20

var (
	idRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	packRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`) // catalog ids of the feed format
	colorRe  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	holderRe = regexp.MustCompile(`\{[^{}]*\}`)
	appIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+){2,}$`)
	keyRe    = regexp.MustCompile(`^ed25519:[A-Za-z0-9_-]{43}$`)
	kindRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`) // pack kinds of the feed format
	extRe    = regexp.MustCompile(`^\.[a-z0-9]{1,8}$`)
)

// startVars are the placeholders a start command may use.
var startVars = []string{"library", "appdir", "userdata"}

// Config is a checked launcher.json (version 1). Unknown fields are ignored.
type Config struct {
	Config          int                  `json:"config"`
	ID              string               `json:"id"`
	Name            Text                 `json:"name"` // "Karaoke" or {"en": "Karaoke", "ja": "カラオケ"}
	Look            Look                 `json:"look"`
	Links           []Link               `json:"links"`
	News            *News                `json:"news"`
	Feeds           []Feed               `json:"feeds"`
	AllowOtherFeeds bool                 `json:"allow_other_feeds"`
	Library         Library              `json:"library"`
	Start           map[string][]string  `json:"start"`  // "windows", "linux", "macos" -> arguments
	AppID           string               `json:"app_id"` // reverse-DNS name, e.g. "org.example.Karaoke"
	Updates         *Updates             `json:"updates"`
	Components      map[string]Component `json:"components"` // component id -> where it is installed
	Content         map[string]Content   `json:"content"`    // pack kind (for example "theme") -> where it goes

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

// Updates says where the community publishes its signed app.json (program updates).
type Updates struct {
	URL  string `json:"url"`
	Key  string `json:"key"`  // "ed25519:<base64url>": app.json must be signed with it
	Page string `json:"page"` // optional: where users get a new version by hand
}

// Component is a program the launcher installs and updates besides itself, for example the game.
type Component struct {
	Dir  map[string]string `json:"dir"`  // "windows", "linux", "macos" -> folder
	Keep []string          `json:"keep"` // patterns (path.Match) of files that are never overwritten or deleted
}

// Content says where packs of another kind than songs go (SPEC 5.6 of the feed format), for example themes.
type Content struct {
	Label      Text              `json:"label"`      // heading on the songs screen
	Dir        map[string]string `json:"dir"`        // "windows", "linux", "macos" -> folder
	Extensions []string          `json:"extensions"` // file types allowed there, for example ".ini"
}

// Launcher is the implicit component id of the launcher itself; its folder is {appdir}.
const Launcher = "launcher"

// Library sets where songs go by default.
type Library struct {
	Default string `json:"default"`
}

// Vars are the values of the placeholders.
type Vars struct {
	Library  string // {library}: the song folder the user chose (songs are in <song folder>/<feed id>/)
	AppDir   string // {appdir}: the launcher's folder (see AppDir)
	UserData string // {userdata}: the per-user data folder
	Home     string // {home}: the user's home folder (only in component folders)
}

// AppDir returns the launcher's folder for its program file exe: the folder of exe, or in a macOS app
// (<Name>.app/Contents/MacOS/<exe>) Contents/Resources when launcher.json is there, because Contents/MacOS may only
// hold programs (Apple TN2206, macOS Code Signing In Depth).
func AppDir(exe, goos string) string {
	dir := filepath.Dir(exe)
	if goos == "darwin" && filepath.Base(dir) == "MacOS" {
		res := filepath.Join(filepath.Dir(dir), "Resources")
		if fi, err := os.Stat(filepath.Join(res, "launcher.json")); err == nil && fi.Mode().IsRegular() {
			return res
		}
	}
	return dir
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
	return Parse(data, path)
}

// Parse checks the content of a launcher.json; path is where it is (its folder is the config folder, and errors
// name it). karaoke-bundle uses it for a launcher.json merged with a game definition.
func Parse(data []byte, path string) (*Config, error) {
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
		if !LocalPath(p) {
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
			if _, err := Expand(a, Vars{Library: "l", AppDir: "a", UserData: "u"}, startVars...); err != nil {
				return fmt.Errorf("start.%s: %v", system, err)
			}
		}
	}
	if err := c.checkUpdates(); err != nil {
		return err
	}
	return c.checkContent()
}

func (c *Config) checkContent() error {
	used := map[string]string{} // system + folder -> kind
	for _, kind := range slices.Sorted(maps.Keys(c.Content)) {
		ct := c.Content[kind]
		if !kindRe.MatchString(kind) || kind == "songs" {
			return fmt.Errorf(`content: %q must be a pack kind (1 to 32 characters: a-z, 0-9 and "-") other than "songs"`, kind)
		}
		if len(ct.Label) == 0 {
			return fmt.Errorf("content.%s.label is missing", kind)
		}
		for code, s := range ct.Label {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("content.%s.label for %q is empty", kind, code)
			}
		}
		if err := checkDirs("content."+kind, ct.Dir, "appdir", "userdata", "home", "library"); err != nil {
			return err
		}
		for system, dir := range ct.Dir {
			d := strings.TrimRight(strings.ReplaceAll(dir, `\`, "/"), "/")
			if d == "{library}" {
				return fmt.Errorf("content.%s.dir.%s must not be the song folder itself; use a folder inside it, like {library}/%ss", kind, system, kind)
			}
			if rest, ok := strings.CutPrefix(d, "{library}/"); ok {
				first, _, _ := strings.Cut(rest, "/")
				if strings.HasPrefix(first, "added-") || slices.ContainsFunc(c.Feeds, func(f Feed) bool { return f.ID == first }) {
					return fmt.Errorf("content.%s.dir.%s %q is the folder of a feed's songs; choose another name than %q", kind, system, dir, first)
				}
			}
			key := system + "\x00" + strings.ToLower(d)
			if other, dup := used[key]; dup {
				return fmt.Errorf("content.%s.dir.%s is the same folder as for %q: every kind needs its own folder", kind, system, other)
			}
			used[key] = kind
		}
		if len(ct.Extensions) == 0 {
			return fmt.Errorf("content.%s.extensions is empty: list the file types the game reads there, like \".ini\"", kind)
		}
		for _, e := range ct.Extensions {
			if !extRe.MatchString(e) || kfeed.Blocked(e) {
				return fmt.Errorf(`content.%s.extensions: %q must look like ".png" (lower case) and must not be a program or script type`, kind, e)
			}
		}
	}
	return nil
}

// checkDirs checks a folder per system: it must start with one of the allowed placeholders and contain no "..".
func checkDirs(field string, dirs map[string]string, allowed ...string) error {
	for system, dir := range dirs {
		if system != "windows" && system != "linux" && system != "macos" {
			continue // a later version may know more systems
		}
		if !slices.ContainsFunc(allowed, func(a string) bool { return strings.HasPrefix(dir, "{"+a+"}") }) {
			return fmt.Errorf("%s.dir.%s %q must start with {%s}", field, system, dir, strings.Join(allowed, "}, {"))
		}
		if _, err := Expand(dir, Vars{Library: "l", AppDir: "a", UserData: "u", Home: "h"}, allowed...); err != nil {
			return fmt.Errorf("%s.dir.%s: %v", field, system, err)
		}
		if slices.Contains(strings.Split(strings.ReplaceAll(dir, `\`, "/"), "/"), "..") {
			return fmt.Errorf(`%s.dir.%s %q must not contain ".."`, field, system, dir)
		}
	}
	return nil
}

func (c *Config) checkUpdates() error {
	if c.AppID != "" && (len(c.AppID) > 255 || !appIDRe.MatchString(c.AppID)) {
		return fmt.Errorf(`"app_id" %q must be a reverse-DNS name like "org.example.Karaoke": at least three parts joined by ".", each of a-z, A-Z, 0-9, "_" and "-"`, c.AppID)
	}
	if u := c.Updates; u != nil {
		if err := CheckURL(u.URL); err != nil {
			return fmt.Errorf("updates.url: %v", err)
		}
		if u.Page != "" {
			if err := CheckURL(u.Page); err != nil {
				return fmt.Errorf("updates.page: %v", err)
			}
		}
		if !keyRe.MatchString(u.Key) {
			return errors.New(`updates.key must be "ed25519:" followed by 43 characters (the public key that signs app.json)`)
		}
	}
	for id, comp := range c.Components {
		if !idRe.MatchString(id) {
			return fmt.Errorf("components: %q must be 1 to 32 characters: a-z, 0-9 and \"-\"", id)
		}
		if id == Launcher {
			return fmt.Errorf(`components: %q is the launcher itself and can't be configured`, id)
		}
		if err := checkDirs("components."+id, comp.Dir, "appdir", "userdata", "home"); err != nil {
			return err
		}
		for _, p := range comp.Keep {
			if _, err := path.Match(p, ""); err != nil || p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) ||
				slices.Contains(strings.Split(p, "/"), "..") {
				return fmt.Errorf(`components.%s.keep: %q must be a pattern like "config.ini" or "*.db" (relative, with "/", no "..")`, id, p)
			}
		}
	}
	return nil
}

// LocalPath reports whether p is a relative path that stays inside its folder, on every system.
func LocalPath(p string) bool {
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
	values := map[string]string{"library": v.Library, "appdir": v.AppDir, "userdata": v.UserData, "home": v.Home}
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
		e, err := Expand(a, v, startVars...)
		if err != nil {
			return nil, true, err
		}
		args = append(args, e)
	}
	return args, true, nil
}

// ContentDir returns the folder for a pack kind on a Go system name (runtime.GOOS), with placeholders replaced.
// ok is false when the kind is not configured or has no folder for this system.
func (c *Config) ContentDir(kind, goos string, v Vars) (dir string, ok bool, err error) {
	system := map[string]string{"windows": "windows", "linux": "linux", "darwin": "macos"}[goos]
	raw, ok := c.Content[kind].Dir[system]
	if !ok || system == "" {
		return "", false, nil
	}
	s, err := Expand(raw, v, "appdir", "userdata", "home", "library")
	if err != nil {
		return "", true, err
	}
	return filepath.Clean(filepath.FromSlash(s)), true, nil
}

// ContentInLibrary reports whether the folder for a pack kind on a Go system name is inside the song folder
// ({library}/…), so that it moves along when the user chooses another song folder.
func (c *Config) ContentInLibrary(kind, goos string) bool {
	system := map[string]string{"windows": "windows", "linux": "linux", "darwin": "macos"}[goos]
	return strings.HasPrefix(c.Content[kind].Dir[system], "{library}")
}

// Targets returns where packs of other kinds go on this system, for the engine. A kind without a folder for this
// system, or whose folder can't be built, is left out.
func (c *Config) Targets(goos string, v Vars) map[string]kfeed.Target {
	t := map[string]kfeed.Target{}
	for kind, ct := range c.Content {
		if dir, ok, err := c.ContentDir(kind, goos, v); ok && err == nil {
			t[kind] = kfeed.Target{Dir: dir, Extensions: slices.Clone(ct.Extensions)}
		}
	}
	return t
}

// ComponentDir returns the folder of a component for a Go system name (runtime.GOOS), with placeholders replaced.
// The launcher's folder is {appdir}. ok is false when the component is unknown or has no folder for this system.
func (c *Config) ComponentDir(id, goos string, v Vars) (dir string, ok bool, err error) {
	if id == Launcher {
		return filepath.Clean(v.AppDir), true, nil
	}
	system := map[string]string{"windows": "windows", "linux": "linux", "darwin": "macos"}[goos]
	raw, ok := c.Components[id].Dir[system]
	if !ok || system == "" {
		return "", false, nil
	}
	s, err := Expand(raw, v, "appdir", "userdata", "home")
	if err != nil {
		return "", true, err
	}
	return filepath.Clean(filepath.FromSlash(s)), true, nil
}
