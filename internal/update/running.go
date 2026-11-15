package update

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Running returns the programs that run from dir or a folder inside it (their file names, each once), for example
// a game that is still open. ok is false on systems where the launcher can't see that (it then returns nil); the
// launcher's own process is never listed.
func Running(dir string) (names []string, ok bool) {
	exes, ok := processPaths()
	if !ok {
		return nil, false
	}
	root := dir
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		root = r
	}
	for _, exe := range exes {
		if within(exe, root) || within(exe, dir) {
			if n := filepath.Base(exe); !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	slices.Sort(names)
	return names, true
}

// within reports whether file is inside dir (upper and lower case count as the same on Windows and macOS).
func within(file, dir string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		file, dir = strings.ToLower(file), strings.ToLower(dir)
	}
	rel, err := filepath.Rel(dir, file)
	return err == nil && filepath.IsLocal(rel)
}

// selfPID is the launcher's own process, which is never listed.
var selfPID = os.Getpid()
