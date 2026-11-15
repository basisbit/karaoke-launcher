// Package folders marks the folders the launcher fills (the song folder, the folders for themes and other content)
// with a README.md. People see what a folder is for, and the launcher sees when a folder is chosen for a second
// purpose.
package folders

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Name is the file name of the marker.
const Name = "README.md"

// Songs is the purpose of the song folder.
const Songs = "songs"

// Content returns the purpose of the folder for packs of a kind, for example "content:theme".
func Content(kind string) string { return "content:" + kind }

// ErrOther means the folder is marked for another purpose.
var ErrOther = errors.New("the folder is used for something else")

var markRe = regexp.MustCompile(`<!-- karaoke-launcher: ([a-z0-9:-]{1,64}) -->`)

const maxSize = 64 << 10

// read returns the README.md of dir; exists is false when there is none.
func read(dir string) (text string, exists bool) {
	f, err := os.Open(filepath.Join(dir, Name))
	if err != nil {
		return "", !errors.Is(err, os.ErrNotExist)
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, maxSize))
	return string(data), true
}

// Purpose returns what dir was marked for, or "" when it has no README.md of the launcher.
func Purpose(dir string) string {
	text, _ := read(dir)
	if m := markRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// Mark writes a README.md with text and the marker for purpose into dir, which must exist. A README.md that is not
// the launcher's is left alone; one marked for another purpose too, and then ErrOther is returned. One of the same
// purpose is replaced when the text changed (another language).
func Mark(dir, purpose, text string) error {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return err
	}
	want := strings.TrimRight(text, "\n") + "\n\n<!-- karaoke-launcher: " + purpose + " -->\n"
	have, exists := read(dir)
	if exists {
		switch p := Purpose(dir); {
		case p == "":
			return nil // the user's or the game's own README.md
		case p != purpose:
			return ErrOther
		case have == want:
			return nil
		}
	}
	return os.WriteFile(filepath.Join(dir, Name), []byte(want), 0o644)
}

// Within reports whether dir is parent or a folder inside it. Both are made absolute; on Windows and macOS upper and
// lower case count as the same.
func Within(dir, parent string) bool {
	d, err1 := filepath.Abs(dir)
	p, err2 := filepath.Abs(parent)
	if err1 != nil || err2 != nil {
		return false
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		d, p = strings.ToLower(d), strings.ToLower(p)
	}
	rel, err := filepath.Rel(p, d)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// Overlap reports whether a and b are the same folder or one is inside the other.
func Overlap(a, b string) bool { return Within(a, b) || Within(b, a) }

// MarkedParent returns the purpose of the nearest folder above dir that the launcher marked, or "".
func MarkedParent(dir string) string {
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for p := filepath.Dir(d); p != d; d, p = p, filepath.Dir(p) {
		if purpose := Purpose(p); purpose != "" {
			return purpose
		}
	}
	return ""
}
