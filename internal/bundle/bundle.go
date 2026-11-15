// Package bundle turns a community's launcher.json and bundle.json into the files it publishes: a Windows
// installer and portable zip, the update site and so on (cmd/karaoke-bundle). See docs/bundles.md.
package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/update"
)

// Bundle is a checked bundle.json with the launcher.json next to it.
type Bundle struct {
	Bundle      int                  `json:"bundle"`
	Version     string               `json:"version"`
	Icon        string               `json:"icon"` // PNG, square, at least 256 px; relative to bundle.json
	Summary     string               `json:"summary"`
	Description string               `json:"description"`
	License     string               `json:"license"`
	Homepage    string               `json:"homepage"`
	Developer   string               `json:"developer"`
	Site        string               `json:"site"` // public address of dist/site/, ends with "/"
	Launcher    Release              `json:"launcher"`
	Files       []string             `json:"files"` // shipped files and folders, relative to bundle.json
	Components  map[string]Component `json:"components"`
	Flatpak     *Flatpak             `json:"flatpak"` // only for the flatpak command
	Mac         *Macos               `json:"macos"`   // only for the macos command
	GameRef     *GameRef             `json:"game"`    // the game definition merged into bundle.json and launcher.json

	Dir          string         `json:"-"` // folder of bundle.json (absolute)
	Config       *config.Config `json:"-"` // launcher.json in Dir, merged with the game definition
	LauncherJSON []byte         `json:"-"` // the text of that launcher.json: what the packages get
	Game         *Game          `json:"-"` // the game definition; nil without "game"
	Shipped      []Shipped      `json:"-"` // launcher.json, the files its look refers to, and Files; sorted by Rel
}

// Macos is what only the macOS app gets.
type Macos struct {
	Apps []MacApp `json:"apps"` // apps copied into the launcher's app (Contents/Helpers/<app>)
}

// MacApp is an app inside a .dmg file on the web (Apple's usual way to hand out apps).
type MacApp struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	App    string `json:"app"` // the folder name of the app in the .dmg, like "Game.app"
}

// Release is the launcher release a bundle ships.
type Release struct {
	Version string `json:"version"` // a release tag, for example "v0.2.0"
	Repo    string `json:"repo"`    // "<owner>/<name>" on GitHub
}

// Flatpak says how the Flatpak is built (docs.flatpak.org, manifests).
type Flatpak struct {
	Runtime        string         `json:"runtime"`         // for example "org.freedesktop.Platform"
	RuntimeVersion string         `json:"runtime_version"` // for example "26.08"
	Sdk            string         `json:"sdk"`             // for example "org.freedesktop.Sdk"
	FinishArgs     []string       `json:"finish_args"`     // permissions, for example "--share=network"
	Modules        []string       `json:"modules"`         // the community's flatpak-builder module files, relative to bundle.json
	SdkExtensions  []string       `json:"sdk_extensions"`  // manifest "sdk-extensions", for example a compiler
	BuildOptions   map[string]any `json:"build_options"`   // manifest "build-options" (for all modules)
	Cleanup        []string       `json:"cleanup"`         // manifest "cleanup" (files removed after all modules are built)
}

// Component is the version of a program and its archive per system ("<GOOS>-<GOARCH>").
type Component struct {
	Version string
	Files   map[string]File
}

// File is the archive of a component for one system: on the web (URL) or next to bundle.json (Path).
type File struct {
	URL    string `json:"url"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Strip  int    `json:"strip"`
}

// Shipped is a file that goes into every package next to the launcher.
type Shipped struct {
	Rel  string // path inside the launcher's folder, with "/"
	Abs  string // where it is now
	Data []byte // the content when it is made by the tool (the merged launcher.json, the game's notice); else nil
}

// copyTo writes the shipped file to dst.
func (s Shipped) copyTo(dst string) error {
	if s.Data == nil {
		return copyFile(s.Abs, dst, 0o644)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, s.Data, 0o644)
}

// UnmarshalJSON reads {"version": "…", "<GOOS>-<GOARCH>": {file}, …}.
func (c *Component) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.Files = map[string]File{}
	for k, v := range raw {
		if k == "version" {
			if err := json.Unmarshal(v, &c.Version); err != nil {
				return fmt.Errorf(`"version": %v`, err)
			}
			continue
		}
		var f File
		d := json.NewDecoder(bytes.NewReader(v))
		d.DisallowUnknownFields()
		if err := d.Decode(&f); err != nil {
			return fmt.Errorf("%q: %v", k, err)
		}
		c.Files[k] = f
	}
	return nil
}

// Problems is every problem found in a bundle, one sentence each.
type Problems []string

func (p Problems) Error() string { return strings.Join(p, "\n") }

const maxBundle = 1 << 20

var (
	versionRe  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)
	tagRe      = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$`)
	repoRe     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	platformRe = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)
	hashRe     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Load reads and checks bundle.json and the launcher.json in the same folder. All problems are returned at once
// (Problems).
func Load(file string) (*Bundle, error) {
	data, err := readLimited(file, maxBundle)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(filepath.Dir(file))
	if err != nil {
		return nil, err
	}
	raw, err := decodeObject(data)
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid bundle.json: %v", file, err)
	}
	var game *Game
	if ref, ok := raw["game"]; ok {
		var r GameRef
		js, _ := json.Marshal(ref)
		if err := json.Unmarshal(js, &r); err != nil {
			return nil, fmt.Errorf("%s: %v", file, err)
		}
		if game, err = loadGame(r, dir); err != nil {
			return nil, err
		}
		raw = mergeJSON(game.Bundle, raw, "").(map[string]any)
		data, _ = json.Marshal(raw)
	}
	var b Bundle
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields() // a misspelt field would be ignored silently
	if err := d.Decode(&b); err != nil {
		return nil, fmt.Errorf("%s is not a valid bundle.json: %v", file, err)
	}
	b.Dir, b.Game = dir, game
	var p Problems
	if err := b.loadLauncher(); err != nil {
		p = append(p, err.Error())
	}
	p = append(p, b.check()...)
	if len(p) > 0 {
		return nil, p
	}
	return &b, nil
}

// loadLauncher reads launcher.json, merges the game definition into it and checks it.
func (b *Bundle) loadLauncher() error {
	file := filepath.Join(b.Dir, "launcher.json")
	data, err := readLimited(file, config.MaxSize)
	if err != nil {
		return err
	}
	if b.Game != nil {
		raw, err := decodeObject(data)
		if err != nil {
			return fmt.Errorf("%s is not valid JSON: %v", file, err)
		}
		if data, err = json.MarshalIndent(mergeJSON(b.Game.Launcher, raw, ""), "", "  "); err != nil {
			return err
		}
		data = append(data, '\n')
	}
	b.LauncherJSON = data
	b.Config, err = config.Parse(data, file)
	return err
}

func (b *Bundle) check() (p Problems) {
	add := func(format string, a ...any) { p = append(p, fmt.Sprintf(format, a...)) }
	if b.Bundle != 1 {
		add(`"bundle" must be 1, not %d.`, b.Bundle)
	}
	if !versionRe.MatchString(b.Version) {
		add(`"version" %q must be 1 to 64 letters, digits, ".", "_", "+" or "-", like "1.2.0".`, b.Version)
	}
	for name, v := range map[string]string{"summary": b.Summary, "description": b.Description, "license": b.License, "developer": b.Developer} {
		if strings.TrimSpace(v) == "" {
			add(`"%s" is missing.`, name)
		}
	}
	if strings.ContainsAny(b.Summary, "\r\n") {
		add(`"summary" must be one line.`)
	}
	if err := config.CheckURL(b.Homepage); err != nil {
		add(`"homepage": %v.`, err)
	}
	if err := config.CheckURL(b.Site); err != nil || !strings.HasSuffix(b.Site, "/") {
		add(`"site" %q must be an https address that ends with "/" (http only for 127.0.0.1 or localhost).`, b.Site)
	} else if c := b.Config; c != nil && c.Updates != nil && c.Updates.URL != b.Site+"app.json" {
		add(`"updates.url" of launcher.json is %q, but the site publishes %q.`, c.Updates.URL, b.Site+"app.json")
	}
	if !tagRe.MatchString(b.Launcher.Version) || !repoRe.MatchString(b.Launcher.Repo) {
		add(`"launcher" needs "version" (a release tag like "v0.2.0") and "repo" (like "owner/karaoke-launcher").`)
	}
	p = append(p, b.checkIcon()...)
	p = append(p, b.checkFlatpak()...)
	p = append(p, b.checkMacos()...)
	p = append(p, b.collect()...)
	p = append(p, b.checkComponents()...)
	return p
}

var flatpakNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)

func (b *Bundle) checkFlatpak() (p Problems) {
	f := b.Flatpak
	if f == nil {
		return nil
	}
	add := func(format string, a ...any) { p = append(p, fmt.Sprintf(format, a...)) }
	if b.Config != nil && b.Config.AppID == "" {
		add(`"flatpak" needs "app_id" in launcher.json.`)
	}
	if !flatpakNameRe.MatchString(f.Runtime) || !flatpakNameRe.MatchString(f.RuntimeVersion) || !flatpakNameRe.MatchString(f.Sdk) {
		add(`"flatpak" needs "runtime", "runtime_version" and "sdk", like "org.freedesktop.Platform", "26.08", "org.freedesktop.Sdk".`)
	}
	for _, e := range f.SdkExtensions {
		if !flatpakNameRe.MatchString(e) {
			add(`"flatpak.sdk_extensions": %q must be an extension id like "org.freedesktop.Sdk.Extension.freepascal".`, e)
		}
	}
	for _, c := range f.Cleanup {
		if strings.TrimSpace(c) == "" || strings.ContainsAny(c, "\r\n") {
			add(`"flatpak.cleanup": %q must be a file pattern like "/include".`, c)
		}
	}
	for _, a := range f.FinishArgs {
		if !strings.HasPrefix(a, "--") || strings.ContainsAny(a, " \t\r\n") {
			add(`"flatpak.finish_args": %q must be one option like "--share=network".`, a)
		}
	}
	for _, m := range f.Modules {
		full, err := b.local(m)
		if err == nil {
			_, err = os.Stat(full)
		}
		if err != nil {
			add(`"flatpak.modules": %v.`, err)
		}
	}
	return p
}

var appNameRe = regexp.MustCompile(`^[^/\\:]+\.app$`)

func (b *Bundle) checkMacos() (p Problems) {
	if b.Mac == nil {
		return nil
	}
	for i, a := range b.Mac.Apps {
		where := fmt.Sprintf(`"macos.apps[%d]"`, i)
		if err := config.CheckURL(a.URL); err != nil {
			p = append(p, fmt.Sprintf("%s: %v.", where, err))
		} else if !strings.HasSuffix(strings.ToLower(a.URL), ".dmg") {
			p = append(p, fmt.Sprintf("%s: %s is not a .dmg file.", where, a.URL))
		}
		if !hashRe.MatchString(a.SHA256) {
			p = append(p, fmt.Sprintf(`%s needs "sha256" ("sha256:" and 64 hex characters).`, where))
		}
		if !appNameRe.MatchString(a.App) || a.App == ".app" {
			p = append(p, fmt.Sprintf(`%s: "app" %q must be the name of the app in the .dmg, like "Game.app".`, where, a.App))
		}
	}
	return p
}

func (b *Bundle) checkIcon() Problems {
	full, err := b.local(b.Icon)
	if err != nil {
		return Problems{fmt.Sprintf(`"icon": %v.`, err)}
	}
	f, err := os.Open(full)
	if err != nil {
		return Problems{fmt.Sprintf(`"icon": %v.`, err)}
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	switch {
	case err != nil:
		return Problems{fmt.Sprintf(`"icon" %s is not a PNG file.`, b.Icon)}
	case cfg.Width != cfg.Height || cfg.Width < 256:
		return Problems{fmt.Sprintf(`"icon" %s is %dx%d pixels; it must be square and at least 256x256.`, b.Icon, cfg.Width, cfg.Height)}
	}
	return nil
}

// local checks a path from bundle.json (relative, with "/", inside its folder) and returns it as a full path.
func (b *Bundle) local(rel string) (string, error) {
	if !safeRel(rel) {
		return "", fmt.Errorf("%q must be a path inside the folder of bundle.json, with \"/\"", rel)
	}
	return filepath.Join(b.Dir, filepath.FromSlash(rel)), nil
}

// safeRel reports whether p is a relative path with "/" that stays inside its folder on every system.
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

// collect fills b.Shipped: launcher.json, the files its look refers to, and everything under Files. Links and
// names the launcher keeps for itself are refused.
func (b *Bundle) collect() (p Problems) {
	seen := map[string]bool{}
	add := func(rel string) {
		key := strings.ToLower(rel)
		if seen[key] {
			return
		}
		seen[key] = true
		first := strings.ToLower(strings.Split(rel, "/")[0])
		if strings.HasPrefix(first, ".component-") || strings.HasPrefix(first, ".update-") || strings.HasPrefix(first, "karaoke-launcher") {
			p = append(p, fmt.Sprintf("%s uses a name the launcher keeps for itself.", rel))
			return
		}
		b.Shipped = append(b.Shipped, Shipped{Rel: rel, Abs: filepath.Join(b.Dir, filepath.FromSlash(rel))})
	}
	add("launcher.json")
	b.Shipped[0].Data = b.LauncherJSON
	if b.Game != nil {
		add(b.Game.noticeRel())
		b.Shipped[len(b.Shipped)-1].Data = b.Game.notice()
	}
	if c := b.Config; c != nil {
		for _, rel := range []string{c.Look.Logo, c.Look.Background, c.Look.Font} {
			if rel != "" {
				if safeRel(rel) {
					add(rel)
				} else {
					p = append(p, fmt.Sprintf("launcher.json refers to %q, which is not inside its folder.", rel))
				}
			}
		}
	}
	for _, rel := range b.Files {
		full, err := b.local(rel)
		if err != nil {
			p = append(p, fmt.Sprintf(`"files": %v.`, err))
			continue
		}
		err = filepath.WalkDir(full, func(name string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			r, _ := filepath.Rel(b.Dir, name)
			switch {
			case e.Type()&fs.ModeSymlink != 0:
				return fmt.Errorf("%s is a link; only files and folders can be shipped", filepath.ToSlash(r))
			case e.Type().IsRegular():
				add(filepath.ToSlash(r))
			case !e.IsDir():
				return fmt.Errorf("%s is not a file or folder", filepath.ToSlash(r))
			}
			return nil
		})
		if err != nil {
			p = append(p, fmt.Sprintf(`"files": %v.`, err))
		}
	}
	for _, s := range b.Shipped {
		if s.Data != nil && s.Rel != "launcher.json" {
			continue // made by the tool
		}
		if fi, err := os.Lstat(s.Abs); err != nil || !fi.Mode().IsRegular() {
			p = append(p, fmt.Sprintf("%s is missing or not a file.", s.Rel))
		}
	}
	sort.Slice(b.Shipped, func(i, j int) bool { return b.Shipped[i].Rel < b.Shipped[j].Rel })
	return p
}

func (b *Bundle) checkComponents() (p Problems) {
	add := func(format string, a ...any) { p = append(p, fmt.Sprintf(format, a...)) }
	for _, id := range slices.Sorted(maps.Keys(b.Components)) {
		c := b.Components[id]
		if _, ok := b.configComponent(id); !ok && b.Config != nil { // without launcher.json its problem is shown
			add(`Component %q is not in "components" of launcher.json.`, id)
			continue
		}
		if !versionRe.MatchString(c.Version) {
			add(`Component %s: "version" %q must be 1 to 64 letters, digits, ".", "_", "+" or "-".`, id, c.Version)
		}
		if len(c.Files) == 0 {
			add(`Component %s has no archive for any system.`, id)
		}
		for _, platform := range slices.Sorted(maps.Keys(c.Files)) {
			f := c.Files[platform]
			where := fmt.Sprintf("Component %s, %s", id, platform)
			if !platformRe.MatchString(platform) {
				add(`%s: the system must be written like "windows-amd64".`, where)
			}
			if !hashRe.MatchString(f.SHA256) || f.Strip < 0 {
				add(`%s needs "sha256" ("sha256:" and 64 hex characters) and "strip" of 0 or more.`, where)
			}
			switch {
			case (f.URL == "") == (f.Path == ""):
				add(`%s needs either "url" or "path".`, where)
			case f.URL != "":
				if err := config.CheckURL(f.URL); err != nil {
					add(`%s: %v.`, where, err)
				} else if update.Ending(f.URL) == "" {
					add(`%s: %s is not a .zip, .tar.gz or .tgz file.`, where, f.URL)
				}
			default:
				p = append(p, b.checkLocalArchive(where, f)...)
			}
		}
	}
	return p
}

func (b *Bundle) checkLocalArchive(where string, f File) Problems {
	full, err := b.local(f.Path)
	if err != nil {
		return Problems{fmt.Sprintf(`%s: "path" %v.`, where, err)}
	}
	if update.Ending(f.Path) == "" {
		return Problems{fmt.Sprintf(`%s: %s is not a .zip, .tar.gz or .tgz file.`, where, f.Path)}
	}
	sum, _, err := hashFile(full)
	if err != nil {
		return Problems{fmt.Sprintf(`%s: %v.`, where, err)}
	}
	if hashRe.MatchString(f.SHA256) && sum != f.SHA256 {
		return Problems{fmt.Sprintf(`%s: %s has the hash %s, but bundle.json says %s.`, where, f.Path, sum, f.SHA256)}
	}
	return nil
}

// configComponent returns the component of launcher.json; the launcher itself is never one.
func (b *Bundle) configComponent(id string) (config.Component, bool) {
	if b.Config == nil || id == config.Launcher {
		return config.Component{}, false
	}
	c, ok := b.Config.Components[id]
	return c, ok
}

// hashFile returns "sha256:<hex>" and the size of a file.
func hashFile(name string) (string, int64, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), n, nil
}

func readLimited(name string, max int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(data)) > max {
		err = errors.New(name + " is too large")
	}
	return data, err
}
