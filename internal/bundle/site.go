package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	kfeed "github.com/basisbit/karaoke-feeds/tool"
	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/update"
)

// BuildSite writes <Out>/site/, the folder to copy to the web server at "site": app.json signed with keyFile ("-" reads
// the key from standard input) in sig/<hash>.sig, files/ (the launcher archives the other commands made and the
// component archives given by "path") and download/ (installers and zips for people). It returns notes for the
// user, for example when no launcher archive was found.
func (b *Bundle) BuildSite(ctx context.Context, o Options, keyFile string) (notes []string, err error) {
	if b.Config.Updates == nil {
		return nil, errors.New(`launcher.json has no "updates": the launcher would never read app.json`)
	}
	out := filepath.Join(o.Out, "site")
	if err := os.RemoveAll(out); err != nil {
		return nil, err
	}
	for _, d := range []string{"files", "download", "sig"} {
		if err := os.MkdirAll(filepath.Join(out, d), 0o755); err != nil {
			return nil, err
		}
	}
	m := update.Manifest{App: 1, ID: b.Config.ID, Sequence: time.Now().Unix(), Components: map[string]update.Component{}}

	// the launcher: the archives that the commands for each system wrote
	launcher := update.Component{Version: b.Version, Files: map[string]update.File{}}
	found, _ := filepath.Glob(filepath.Join(o.Out, "*", "launcher-"+b.Version+"-*.zip"))
	for _, src := range found {
		platform := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(src), "launcher-"+b.Version+"-"), ".zip")
		if !platformRe.MatchString(platform) {
			continue
		}
		f, err := publish(src, filepath.Join(out, "files", filepath.Base(src)))
		if err != nil {
			return nil, err
		}
		launcher.Files[platform] = f
	}
	if len(launcher.Files) == 0 {
		notes = append(notes, "No launcher archive was found in "+o.Out+": run the commands for the systems first, or the launcher will not update itself.")
	} else {
		m.Components[config.Launcher] = launcher
	}

	// the other components: an archive next to bundle.json is published here, one on the web stays there
	for _, id := range slices.Sorted(maps.Keys(b.Components)) {
		c := b.Components[id]
		uc := update.Component{Version: c.Version, Files: map[string]update.File{}}
		for _, platform := range slices.Sorted(maps.Keys(c.Files)) {
			f := c.Files[platform]
			archive, err := b.componentArchive(ctx, f, filepath.Join(o.Out, "cache"))
			if err != nil {
				return nil, fmt.Errorf("component %s, %s: %v", id, platform, err)
			}
			if f.URL != "" {
				_, size, err := hashFile(archive) // app.json needs the size; the hash was checked by the download
				if err != nil {
					return nil, err
				}
				uc.Files[platform] = update.File{URL: f.URL, SHA256: f.SHA256, Size: size, Strip: f.Strip}
				continue
			}
			name := fmt.Sprintf("%s-%s-%s%s", id, c.Version, platform, update.Ending(f.Path))
			uf, err := publish(archive, filepath.Join(out, "files", name))
			if err != nil {
				return nil, err
			}
			uf.Strip = f.Strip
			uc.Files[platform] = uf
		}
		m.Components[id] = uc
	}

	// downloads for people: everything the commands wrote as <id>-<version>-…
	downloads, _ := filepath.Glob(filepath.Join(o.Out, "*", b.Config.ID+"-"+b.Version+"-*"))
	for _, src := range downloads {
		if fi, err := os.Stat(src); err == nil && fi.Mode().IsRegular() {
			if err := copyFile(src, filepath.Join(out, "download", filepath.Base(src)), 0o644); err != nil {
				return nil, err
			}
		}
	}
	if len(downloads) == 0 {
		notes = append(notes, "No installer or zip was found in "+o.Out+": download/ is empty.")
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	sig, pub, err := kfeed.Sign(keyFile, data)
	if err != nil {
		return nil, fmt.Errorf("key %s: %v", keyFile, err)
	}
	if pub != b.Config.Updates.Key {
		return nil, fmt.Errorf(`the key %s has the public key %s, but "updates.key" in launcher.json is %s: the launcher would refuse app.json`, keyFile, pub, b.Config.Updates.Key)
	}
	sum := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(out, "sig", hex.EncodeToString(sum[:])+".sig"), []byte(sig), 0o644); err != nil {
		return nil, err
	}
	return notes, os.WriteFile(filepath.Join(out, "app.json"), data, 0o644)
}

// publish copies an archive into the site and returns its entry for app.json (address relative to app.json).
func publish(src, dst string) (update.File, error) {
	if err := copyFile(src, dst, 0o644); err != nil {
		return update.File{}, err
	}
	sum, size, err := hashFile(dst)
	return update.File{URL: "files/" + filepath.Base(dst), SHA256: sum, Size: size}, err
}
