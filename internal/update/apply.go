package update

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// rename is os.Rename; a test replaces it to make a target that can't be replaced.
var rename = os.Rename

// foldCase is true where the file system usually ignores upper and lower case (a test sets it).
var foldCase = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// pathKey is how two paths of a component are compared: "Game.exe" and "game.exe" are one file where case is
// ignored.
func pathKey(rel string) string {
	if foldCase {
		return strings.ToLower(rel)
	}
	return rel
}

// linkFree reports whether no part of rel below dir is a symbolic link or a junction, so that a file written or
// deleted there stays inside dir. Parts that don't exist yet are fine.
func linkFree(dir, rel string) bool {
	cur := dir
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return true
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return false
		}
	}
	return true
}

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
	r, err := staged(dir, id)
	return err == nil && r != nil
}

// staged returns the update staged for the installed version of a component, or nil. A staging made for another
// installed version (a setup installed a different one meanwhile) is deleted: it is never applied over it.
func staged(dir, id string) (*ready, error) {
	var r ready
	if err := readJSON(readyPath(dir, id), &r); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	installed, err := installedVersion(dir, id)
	if err != nil {
		return nil, err
	}
	if r.From != installed {
		return nil, os.RemoveAll(stagingDir(dir, id))
	}
	return &r, nil
}

// installedVersion is the installed version of a component, "" when none is installed.
func installedVersion(dir, id string) (string, error) {
	st, err := Installed(dir, id)
	if st == nil || err != nil {
		return "", err
	}
	return st.Version, nil
}

// Apply installs the unpacked update of a component (no network). It does nothing and returns false when there is
// none. When it is interrupted or fails, the next call completes it. Files the launcher never wrote are never
// deleted, files that match keep are never overwritten, and nothing is written or deleted through a link.
func Apply(dir, id string, keep []string) (bool, error) {
	r, err := staged(dir, id)
	if r == nil || err != nil {
		return false, err
	}
	old, err := Installed(dir, id)
	if err != nil {
		return false, err
	}
	staged := filepath.Join(stagingDir(dir, id), "files")
	have := make(map[string]bool, len(r.Files))
	for _, rel := range r.Files {
		if !safeRel(rel) {
			return false, fmt.Errorf("update of %s lists the unsafe path %q", id, rel)
		}
		if !linkFree(dir, rel) {
			return false, fmt.Errorf("update of %s: %s goes through a link in %s", id, rel, dir)
		}
		have[pathKey(rel)] = true
		// Before anything changes: every file must be staged or in place already (moved by an interrupted run).
		// A staging that lost a file is deleted, so that the next check stages the update again.
		_, serr := os.Lstat(filepath.Join(staged, filepath.FromSlash(rel)))
		_, terr := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
		if errors.Is(serr, fs.ErrNotExist) && errors.Is(terr, fs.ErrNotExist) {
			os.RemoveAll(stagingDir(dir, id))
			return false, fmt.Errorf("update of %s: %s is missing from the downloaded update; it is downloaded again", id, rel)
		}
	}
	// Files of the old version that the new one doesn't have go first, so that a file of the old version can become
	// a folder of the new one and the other way round.
	if old != nil {
		for _, rel := range old.Files {
			if !safeRel(rel) || have[pathKey(rel)] || Keep(keep, rel) || !linkFree(dir, rel) {
				continue
			}
			if err := removeOld(dir, rel); err != nil {
				return false, fmt.Errorf("update of %s: can't remove the old file %s: %w", id, rel, err)
			}
		}
	}
	var list []string
	for _, rel := range r.Files {
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
	if err := WriteState(dir, State{Component: id, Version: r.Version, Files: list}); err != nil {
		return false, err
	}
	return true, os.RemoveAll(stagingDir(dir, id))
}

// removeOld deletes a file of the old version. One that can't be deleted (a running program on Windows) is renamed
// and deleted by Cleanup at the next start. A folder at its place now is not the launcher's and stays.
func removeOld(dir, rel string) error {
	target := filepath.Join(dir, filepath.FromSlash(rel))
	fi, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist): // removed by an interrupted run, or by the user
	case err != nil:
		return err
	case fi.IsDir():
		return nil
	default:
		if err := os.Remove(target); err != nil {
			if merr := moveAside(dir, rel); merr != nil {
				return errors.Join(err, merr)
			}
		}
	}
	removeEmptyParents(dir, rel)
	return nil
}

// replace moves src to target. A file that can't be replaced (a running program on Windows) is renamed first; a
// folder is never moved aside, because it may hold files that are not the launcher's.
func replace(dir, rel, src, target string, exists bool) error {
	err := rename(src, target)
	if err == nil || !exists {
		return err
	}
	if fi, serr := os.Lstat(target); serr == nil && fi.IsDir() {
		return fmt.Errorf("%s is a folder and can't be replaced with a file: %w", rel, err)
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
		if !safeRel(rel) || !strings.Contains(path.Base(rel), ".old-") || !linkFree(dir, rel) {
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
