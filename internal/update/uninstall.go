package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Uninstall removes what the launcher installed of a component in dir: the files its state file lists (the
// installer's and every update's), files moved aside by an update, a staged update and the state file, and the
// folders that are empty then. Everything else stays: files the program wrote itself (settings, scores), files that
// match keep (they are never listed) and anything the user put there. except is a file that stays too (the running
// launcher, which can't delete itself on Windows); "" for none. A file that can't be removed (a program that still
// runs) is left and reported; the rest is still removed, and the state file stays for another run.
func Uninstall(dir, id, except string) error {
	var errs []error
	if err := Cleanup(dir); err != nil {
		errs = append(errs, err)
	}
	st, err := Installed(dir, id)
	if err != nil {
		return err
	}
	var exceptInfo fs.FileInfo
	if except != "" {
		exceptInfo, _ = os.Stat(except)
	}
	if st != nil {
		for _, rel := range st.Files {
			if !safeRel(rel) || !linkFree(dir, rel) {
				continue
			}
			target := filepath.Join(dir, filepath.FromSlash(rel))
			fi, err := os.Lstat(target)
			switch {
			case errors.Is(err, fs.ErrNotExist):
			case err != nil:
				errs = append(errs, err)
			case fi.IsDir(): // a folder at the place of a file is not the launcher's
			case exceptInfo != nil && os.SameFile(fi, exceptInfo):
			default:
				if err := os.Remove(target); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", rel, err))
					continue
				}
			}
			removeEmptyParents(dir, rel)
		}
	}
	if len(errs) == 0 { // else the state file stays, so that another run can finish the job
		if err := os.RemoveAll(stagingDir(dir, id)); err != nil {
			errs = append(errs, err)
		} else if err := os.Remove(statePath(dir, id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s in %s: %w", id, dir, errors.Join(errs...))
	}
	return nil
}
