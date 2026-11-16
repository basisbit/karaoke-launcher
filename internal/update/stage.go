package update

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	kfeed "github.com/basisbit/karaoke-feeds/tool"
)

// Stage downloads an update and unpacks it next to its component, ready for Apply (docs/updates.md, rule 8). An
// interrupted download continues at the next call. progress may be nil.
func (u *Updater) Stage(ctx context.Context, up Update, progress func(done, total int64)) error {
	if r, _ := staged(up.Dir, up.ID); r != nil && r.Version == up.Version && r.SHA256 == up.File.SHA256 {
		return nil // staged already; Apply was not run yet
	}
	from, err := installedVersion(up.Dir, up.ID)
	if err != nil {
		return err
	}
	hexsum := strings.TrimPrefix(up.File.SHA256, "sha256:")
	downloads := filepath.Join(u.UserData, "updates")
	if err := os.MkdirAll(downloads, 0o700); err != nil {
		return err
	}
	addr, err := url.Parse(up.File.URL)
	if err != nil {
		return err
	}
	archive := filepath.Join(downloads, hexsum+Ending(addr.Path))
	if err := kfeed.Fetch(ctx, up.File.URL, up.File.SHA256, up.File.Size, archive, progress); err != nil {
		return err
	}
	if err := os.MkdirAll(up.Dir, 0o755); err != nil {
		return err
	}
	staging := stagingDir(up.Dir, up.ID)
	// An older staged version goes first, with its ready.json: an interruption must not leave its file list next to
	// files of this version.
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	files, err := Unpack(archive, filepath.Join(staging, "files"), up.File.Strip)
	if err != nil {
		os.RemoveAll(staging)
		os.Remove(archive) // the hash was right, so it is the archive itself that is refused
		return err
	}
	if err := writeJSON(readyPath(up.Dir, up.ID), ready{Version: up.Version, From: from, SHA256: up.File.SHA256, Files: files}); err != nil {
		os.RemoveAll(staging)
		return err
	}
	return os.Remove(archive)
}
