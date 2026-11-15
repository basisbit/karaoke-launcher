package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Limits of an archive; tests make them smaller.
var (
	maxEntries       = 100_000
	maxBytes   int64 = 16 << 30
)

// deviceRe matches the names Windows reserves for devices, also with an extension ("nul.txt").
var deviceRe = regexp.MustCompile(`(?i)^(con|prn|aux|nul|conin\$|conout\$|com[0-9¹²³]|lpt[0-9¹²³])(\..*)?$`)

// Ending returns the archive format of a file name: ".zip", ".tar.gz" or ".tgz"; "" for anything else.
func Ending(name string) string {
	lower := strings.ToLower(name)
	for _, e := range []string{".zip", ".tar.gz", ".tgz"} {
		if strings.HasSuffix(lower, e) {
			return e
		}
	}
	return ""
}

// entry is one member of an archive.
type entry struct {
	name string
	dir  bool
	exec bool
	open func() (io.ReadCloser, error) // nil for folders
}

// Unpack extracts a .zip, .tar.gz or .tgz archive into dest (which is created; an old dest is removed first),
// leaving out the first strip parts of every path. It returns the files written, with "/". When one entry is not
// safe, nothing is kept: dest is removed and an error returned.
func Unpack(archive, dest string, strip int) ([]string, error) {
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}
	files, err := unpack(archive, dest, strip)
	if err != nil {
		os.RemoveAll(dest)
		return nil, fmt.Errorf("%s: %v", filepath.Base(archive), err)
	}
	return files, nil
}

func unpack(archive, dest string, strip int) ([]string, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	u := unpacker{dest: dest, strip: strip, seen: map[string]bool{}}
	switch Ending(archive) {
	case ".zip":
		z, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		defer z.Close()
		for _, f := range z.File {
			mode := f.Mode()
			if mode&fs.ModeType&^fs.ModeDir != 0 {
				return nil, fmt.Errorf("%q is a link or a special file", f.Name)
			}
			if err := u.add(entry{name: f.Name, dir: mode.IsDir() || strings.HasSuffix(f.Name, "/"), exec: mode&0o111 != 0, open: f.Open}); err != nil {
				return nil, err
			}
		}
	case ".tar.gz", ".tgz":
		file, err := os.Open(archive)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		gz, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			switch h.Typeflag {
			case tar.TypeXGlobalHeader:
				continue
			case tar.TypeDir:
				err = u.add(entry{name: h.Name, dir: true})
			case tar.TypeReg:
				err = u.add(entry{name: h.Name, exec: h.Mode&0o111 != 0, open: func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }})
			default:
				err = fmt.Errorf("%q is a link or a special file", h.Name)
			}
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, errors.New("not a .zip, .tar.gz or .tgz file")
	}
	return u.files, nil
}

type unpacker struct {
	dest    string
	strip   int
	entries int
	written int64
	seen    map[string]bool // lower-case path -> is a folder
	files   []string
}

func (u *unpacker) add(e entry) error {
	if u.entries++; u.entries > maxEntries {
		return fmt.Errorf("more than %d entries", maxEntries)
	}
	name := strings.TrimSuffix(e.name, "/")
	if err := checkName(name); err != nil {
		return err
	}
	key := strings.ToLower(name)
	if dir, ok := u.seen[key]; ok && !(dir && e.dir) {
		return fmt.Errorf("%q is in the archive twice (upper/lower case ignored)", e.name)
	}
	u.seen[key] = e.dir
	parts := strings.Split(name, "/")
	if len(parts) <= u.strip {
		return nil // a folder that strip removes
	}
	rel := strings.Join(parts[u.strip:], "/")
	if first := parts[u.strip]; strings.HasPrefix(first, ".component-") || strings.HasPrefix(first, ".update-") {
		return fmt.Errorf("%q uses a name the launcher keeps for itself", e.name)
	}
	target := filepath.Join(u.dest, filepath.FromSlash(rel))
	if e.dir {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if e.exec {
		perm = 0o755
	}
	r, err := e.open()
	if err != nil {
		return err
	}
	defer r.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, maxBytes-u.written+1)) // count what is really written
	u.written += n
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("%s: %v", e.name, err)
	}
	if u.written > maxBytes {
		return fmt.Errorf("more than %d bytes when unpacked", maxBytes)
	}
	u.files = append(u.files, rel)
	return nil
}

// checkName refuses a path that could leave the folder or that a system can't store (rule 9 of docs/updates.md).
func checkName(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, `\:`) {
		return fmt.Errorf("%q is not a relative path with \"/\"", name)
	}
	for _, part := range strings.Split(name, "/") {
		switch {
		case part == "" || part == ".." || part == ".":
			return fmt.Errorf("%q has an empty, \".\" or \"..\" part", name)
		case strings.HasSuffix(part, ".") || strings.HasSuffix(part, " "):
			return fmt.Errorf("%q has a part that ends with \".\" or a space", name)
		case deviceRe.MatchString(part):
			return fmt.Errorf("%q uses a name Windows keeps for devices", name)
		case slices.ContainsFunc([]rune(part), func(r rune) bool { return r < 32 }):
			return fmt.Errorf("%q contains a control character", name)
		}
	}
	return nil
}
