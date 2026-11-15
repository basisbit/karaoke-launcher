package update

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// rename is os.Rename; a test replaces it to make a target that can't be replaced.
var rename = os.Rename

// Keep reports whether rel (with "/") matches one of the keep patterns, as a whole path or by its file name.
func Keep(keep []string, rel string) bool {
	for _, p := range keep {
		if ok, _ := path.Match(p, rel); ok {
			return true
		}
		if ok, _ := path.Match(p, path.Base(rel)); ok {
			return true
		}
	}
	return false
}

// Pending reports whether an unpacked update of the component waits to be applied.
func Pending(dir, id string) bool {
	_, err := os.Stat(readyPath(dir, id))
	return err == nil
}

// Apply installs the unpacked update of a component (no network). It does nothing and returns false when there is
// none. When it is interrupted, the next call completes it. Files the launcher never wrote are never deleted, and
// files that match keep are never overwritten.
func Apply(dir, id string, keep []string) (bool, error) {
	var r ready
	if err := readJSON(readyPath(dir, id), &r); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	old, err := Installed(dir, id)
	if err != nil {
		return false, err
	}
	staged := filepath.Join(stagingDir(dir, id), "files")
	var list []string
	for _, rel := range r.Files {
		if !safeRel(rel) {
			return false, fmt.Errorf("update of %s lists the unsafe path %q", id, rel)
		}
		src, target := filepath.Join(staged, filepath.FromSlash(rel)), filepath.Join(dir, filepath.FromSlash(rel))
		_, terr := os.Lstat(target)
		if Keep(keep, rel) {
			if terr == nil {
				os.Remove(src)
				continue
			}
		} else {
			list = append(list, rel)
		}
		if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) && terr == nil {
			continue // moved by an interrupted run
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, err
		}
		if err := replace(dir, rel, src, target, terr == nil); err != nil {
			return false, err
		}
	}
	if old != nil {
		for _, rel := range old.Files {
			if !safeRel(rel) || slices.Contains(list, rel) || Keep(keep, rel) {
				continue
			}
			target := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
				moveAside(dir, rel) // a running program: deleted by Cleanup at the next start
			}
			removeEmptyParents(dir, rel)
		}
	}
	if err := WriteState(dir, State{Component: id, Version: r.Version, Files: list}); err != nil {
		return false, err
	}
	return true, os.RemoveAll(stagingDir(dir, id))
}

// replace moves src to target. A target that can't be replaced (a running program on Windows) is renamed first.
func replace(dir, rel, src, target string, exists bool) error {
	err := rename(src, target)
	if err == nil || !exists {
		return err
	}
	if merr := moveAside(dir, rel); merr != nil {
		return errors.Join(err, merr)
	}
	return rename(src, target)
}

// moveAside renames <dir>/<rel> to <rel>.old-<unix nanoseconds> and lists it in .update-old.txt for Cleanup.
func moveAside(dir, rel string) error {
	old := rel + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := rename(filepath.Join(dir, filepath.FromSlash(rel)), filepath.Join(dir, filepath.FromSlash(old))); err != nil {
		return err
	}
	f, err := os.OpenFile(oldListPath(dir), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(f, old)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// removeEmptyParents removes the folders of rel that are empty now, from the deepest up; never dir itself.
func removeEmptyParents(dir, rel string) {
	for p := path.Dir(rel); p != "." && p != "/"; p = path.Dir(p) {
		if os.Remove(filepath.Join(dir, filepath.FromSlash(p))) != nil {
			return
		}
	}
}

// Cleanup deletes the files that an update moved aside (.update-old.txt). The list is removed when all are gone.
func Cleanup(dir string) error {
	f, err := os.Open(oldListPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var left []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		rel := strings.TrimSpace(sc.Text())
		if !safeRel(rel) || !strings.Contains(path.Base(rel), ".old-") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			left = append(left, rel)
		}
	}
	f.Close()
	if len(left) == 0 {
		return os.Remove(oldListPath(dir))
	}
	return os.WriteFile(oldListPath(dir), []byte(strings.Join(left, "\n")+"\n"), 0o644)
}
