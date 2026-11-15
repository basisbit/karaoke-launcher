package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basisbit/karaoke-launcher/games"
	"github.com/basisbit/karaoke-launcher/internal/config"
)

// A game definition (games/<id>/game.json, docs/bundles.md "Games") holds what a community would otherwise write by
// hand to ship a game: parts of bundle.json (the game's archives, the macOS app, the Flatpak modules) and parts of
// launcher.json (start command, component folder, content folders). "game" in bundle.json picks one; the tool merges
// it into both files. Values the community wrote win; see mergeJSON.

// Game is a checked game definition.
type Game struct {
	Game     int            `json:"game"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Version  string         `json:"version"`
	License  string         `json:"license"`
	Homepage string         `json:"homepage"`
	Source   string         `json:"source"`   // the source code of exactly this version
	Bundle   map[string]any `json:"bundle"`   // parts of bundle.json
	Launcher map[string]any `json:"launcher"` // parts of launcher.json

	FS      fs.FS    `json:"-"` // the definition's folder
	Modules []string `json:"-"` // "flatpak.modules" of Bundle: module files in FS
}

// GameRef is "game" in bundle.json: the id of a definition karaoke-bundle knows ("ultrastar-deluxe"), or
// {"path": "<folder>"}, a folder with game.json next to bundle.json.
type GameRef struct {
	ID   string
	Path string
}

// UnmarshalJSON reads "<id>" or {"path": "<folder>"}.
func (g *GameRef) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &g.ID); err == nil {
		return nil
	}
	var o struct {
		Path string `json:"path"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&o); err != nil || o.Path == "" {
		return fmt.Errorf(`"game" must be the id of a game like "ultrastar-deluxe" or {"path": "<folder with game.json>"}`)
	}
	g.Path = o.Path
	return nil
}

var gameIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// GameIDs returns the ids of the definitions karaoke-bundle knows.
func GameIDs() []string {
	entries, _ := fs.ReadDir(games.FS, ".")
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids
}

// loadGame reads the definition ref names; dir is the folder of bundle.json.
func loadGame(ref GameRef, dir string) (*Game, error) {
	var fsys fs.FS
	where := ref.Path
	if ref.Path != "" {
		if !safeRel(ref.Path) {
			return nil, fmt.Errorf(`"game.path" %q must be a path inside the folder of bundle.json, with "/"`, ref.Path)
		}
		fsys = os.DirFS(filepath.Join(dir, filepath.FromSlash(ref.Path)))
	} else {
		if !gameIDRe.MatchString(ref.ID) {
			return nil, fmt.Errorf(`"game" %q is not a game id`, ref.ID)
		}
		sub, err := fs.Sub(games.FS, ref.ID)
		if err != nil {
			return nil, err
		}
		if _, err := fs.Stat(sub, "game.json"); err != nil {
			return nil, fmt.Errorf(`"game" %q: karaoke-bundle knows no such game; it knows %s`, ref.ID, strings.Join(GameIDs(), ", "))
		}
		fsys, where = sub, "games/"+ref.ID
	}
	data, err := fs.ReadFile(fsys, "game.json")
	if err != nil {
		return nil, fmt.Errorf(`"game": %v`, err)
	}
	var g Game
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(&g); err != nil {
		return nil, fmt.Errorf("%s/game.json is not a valid game definition: %v", where, err)
	}
	g.FS = fsys
	if err := g.check(); err != nil {
		return nil, fmt.Errorf("%s/game.json: %v", where, err)
	}
	return &g, nil
}

func (g *Game) check() error {
	if g.Game != 1 {
		return fmt.Errorf(`"game" must be 1, not %d`, g.Game)
	}
	if !gameIDRe.MatchString(g.ID) || strings.TrimSpace(g.Name) == "" || !versionRe.MatchString(g.Version) || strings.TrimSpace(g.License) == "" {
		return fmt.Errorf(`"id", "name", "version" and "license" are needed`)
	}
	for name, u := range map[string]string{"homepage": g.Homepage, "source": g.Source} {
		if err := config.CheckURL(u); err != nil {
			return fmt.Errorf(`"%s": %v`, name, err)
		}
	}
	// the module files are in the definition's folder, not next to bundle.json: they are handled apart
	if f, ok := g.Bundle["flatpak"].(map[string]any); ok {
		if mods, ok := f["modules"].([]any); ok {
			for _, m := range mods {
				s, _ := m.(string)
				if !safeRel(s) {
					return fmt.Errorf(`"bundle.flatpak.modules": %q must be a path inside the definition's folder`, s)
				}
				if _, err := fs.Stat(g.FS, s); err != nil {
					return fmt.Errorf(`"bundle.flatpak.modules": %v`, err)
				}
				g.Modules = append(g.Modules, s)
			}
			delete(f, "modules")
		}
	}
	return nil
}

// mergeJSON merges over (what the community wrote) into base (the game definition) and returns the result. Objects
// are merged key by key, except a file (an object with "url" or "path", like an archive), which replaces the game's
// as a whole; any other value of over replaces the one of base, except lists inside "flatpak", which are joined (base
// first, each value once): the game's permissions stay when the community adds its own.
func mergeJSON(base, over any, at string) any {
	bm, ok1 := base.(map[string]any)
	om, ok2 := over.(map[string]any)
	_, url := om["url"]
	_, path := om["path"]
	if ok1 && ok2 && !url && !path {
		out := make(map[string]any, len(bm)+len(om))
		for k, v := range bm {
			out[k] = v
		}
		for k, v := range om {
			if bv, ok := bm[k]; ok {
				out[k] = mergeJSON(bv, v, at+"."+k)
			} else {
				out[k] = v
			}
		}
		return out
	}
	ba, ok1 := base.([]any)
	oa, ok2 := over.([]any)
	if ok1 && ok2 && strings.HasPrefix(at, ".flatpak.") {
		out, seen := []any{}, map[string]bool{}
		for _, v := range append(append([]any{}, ba...), oa...) {
			key, _ := json.Marshal(v)
			if !seen[string(key)] {
				seen[string(key)] = true
				out = append(out, v)
			}
		}
		return out
	}
	return over
}

// decodeObject reads a JSON object, keeping numbers as they are written.
func decodeObject(data []byte) (map[string]any, error) {
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("not a JSON object")
	}
	return m, nil
}

// gameNotice is the text of licenses/<game id>.txt in every package: GPL and similar licences ask for the way to
// the source code of exactly the version shipped.
func (g *Game) notice() []byte {
	return []byte(g.Name + " " + g.Version + "\n" +
		"License: " + g.License + " (https://spdx.org/licenses/" + g.License + ".html)\n" +
		"Homepage: " + g.Homepage + "\n" +
		"Source code of this version: " + g.Source + "\n")
}

// gameNoticeRel is where the notice goes in the launcher's folder.
func (g *Game) noticeRel() string { return path.Join("licenses", g.ID+".txt") }
