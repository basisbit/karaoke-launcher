package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// member is one archive entry for the tests.
type member struct {
	name string
	body string
	mode fs.FileMode // 0: 0644 file, or folder when name ends with "/"
	tar  byte        // tar type flag; 0: regular file or folder
	link string
}

func zipFile(t *testing.T, dir string, ms ...member) string {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, m := range ms {
		h := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
		mode := m.mode
		if mode == 0 {
			mode = 0o644
			if strings.HasSuffix(m.name, "/") {
				mode = fs.ModeDir | 0o755
			}
		}
		h.SetMode(mode)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(m.body))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "a.zip")
	os.WriteFile(p, buf.Bytes(), 0o644)
	return p
}

func tgzFile(t *testing.T, dir string, ms ...member) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, m := range ms {
		h := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.body)), Typeflag: m.tar, Linkname: m.link, Format: tar.FormatPAX}
		if m.mode != 0 {
			h.Mode = int64(m.mode.Perm())
		}
		switch {
		case m.tar == 0 && strings.HasSuffix(m.name, "/"):
			h.Typeflag, h.Mode, h.Size = tar.TypeDir, 0o755, 0
		case m.tar == 0:
			h.Typeflag = tar.TypeReg
		case m.tar != tar.TypeReg:
			h.Size = 0
		}
		if m.tar == tar.TypeXGlobalHeader {
			h = &tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: m.name, PAXRecords: map[string]string{"comment": "x"}, Format: tar.FormatPAX}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			tw.Write([]byte(m.body))
		}
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(dir, "a.tar.gz")
	os.WriteFile(p, buf.Bytes(), 0o644)
	return p
}

// tree returns the files below dir with their contents.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			data, _ := os.ReadFile(p)
			got[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	return got
}

func TestUnpack(t *testing.T) {
	good := []member{
		{name: "game-2.0/"},
		{name: "game-2.0/game", body: "#!/bin/sh\n", mode: 0o755},
		{name: "game-2.0/data/a.txt", body: "a"},
		{name: "game-2.0/data/sub/b.txt", body: "bb"},
		{name: "readme.txt", body: "outside the top folder: fewer parts than strip"},
	}
	for _, kind := range []string{"zip", "tgz"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			var arc string
			if kind == "zip" {
				arc = zipFile(t, dir, good...)
			} else {
				arc = tgzFile(t, dir, append([]member{{name: "pax_global_header", tar: tar.TypeXGlobalHeader}}, good...)...)
			}
			dest := filepath.Join(dir, "out")
			os.MkdirAll(filepath.Join(dest, "old"), 0o755) // an old staging folder is replaced
			files, err := Unpack(arc, dest, 1)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(files)
			if want := []string{"data/a.txt", "data/sub/b.txt", "game"}; !slices.Equal(files, want) {
				t.Fatalf("files %q, want %q", files, want)
			}
			if got := tree(t, dest); len(got) != 3 || got["data/sub/b.txt"] != "bb" {
				t.Fatalf("unpacked: %v", got)
			}
			if runtime.GOOS != "windows" {
				for name, want := range map[string]fs.FileMode{"game": 0o755, "data/a.txt": 0o644} {
					if fi, _ := os.Stat(filepath.Join(dest, name)); fi == nil || fi.Mode().Perm() != want {
						t.Errorf("%s: mode %v, want %v", name, fi.Mode().Perm(), want)
					}
				}
			}
			if files, err := Unpack(arc, dest, 0); err != nil || len(files) != 4 {
				t.Fatalf("strip 0: %q %v", files, err)
			}
		})
	}
}

func TestUnpackRefused(t *testing.T) {
	defer func(e int, b int64) { maxEntries, maxBytes = e, b }(maxEntries, maxBytes)
	maxEntries, maxBytes = 5, 100
	ok := member{name: "ok.txt", body: "fine"}
	cases := []struct {
		name string
		m    []member
		tar  bool // tar only
	}{
		{"absolute", []member{ok, {name: "/etc/passwd", body: "x"}}, false},
		{"dot dot", []member{ok, {name: "a/../../x", body: "x"}}, false},
		{"dot dot at start", []member{{name: "../x", body: "x"}}, false},
		{"backslash", []member{{name: `a\..\x`, body: "x"}}, false},
		{"drive", []member{{name: "C:/x", body: "x"}}, false},
		{"colon stream", []member{{name: "a.txt:stream", body: "x"}}, false},
		{"device", []member{{name: "data/con.txt", body: "x"}}, false},
		{"device upper", []member{{name: "NUL", body: "x"}}, false},
		{"device com", []member{{name: "Com1.log", body: "x"}}, false},
		{"trailing dot", []member{{name: "data./a", body: "x"}}, false},
		{"trailing space", []member{{name: "a ", body: "x"}}, false},
		{"case twice", []member{{name: "Game.exe", body: "1"}, {name: "game.EXE", body: "2"}}, false},
		{"same twice", []member{{name: "a", body: "1"}, {name: "a", body: "2"}}, false},
		{"file and folder", []member{{name: "a", body: "1"}, {name: "A/"}}, false},
		{"reserved state name", []member{{name: ".component-game.json", body: "{}"}}, false},
		{"reserved staging name", []member{{name: ".update-launcher/ready.json", body: "{}"}}, false},
		{"reserved name upper case", []member{{name: ".COMPONENT-game.json", body: "{}"}}, false},
		{"reserved staging name mixed case", []member{{name: ".Update-game/ready.json", body: "{}"}}, false},
		{"control character", []member{{name: "a\nb", body: "x"}}, false},
		{"too many entries", []member{{name: "1"}, {name: "2"}, {name: "3"}, {name: "4"}, {name: "5"}, {name: "6"}}, false},
		{"too many bytes", []member{{name: "a", body: strings.Repeat("x", 60)}, {name: "b", body: strings.Repeat("x", 60)}}, false},
		{"zip symlink", []member{ok, {name: "link", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777}}, false},
		{"symlink", []member{ok, {name: "link", tar: tar.TypeSymlink, link: "/etc/passwd"}}, true},
		{"hard link", []member{ok, {name: "link", tar: tar.TypeLink, link: "ok.txt"}}, true},
		{"char device", []member{{name: "dev", tar: tar.TypeChar}}, true},
		{"block device", []member{{name: "dev", tar: tar.TypeBlock}}, true},
		{"fifo", []member{{name: "fifo", tar: tar.TypeFifo}}, true},
	}
	for _, tc := range cases {
		for _, kind := range []string{"zip", "tgz"} {
			if (kind == "zip" && tc.tar) || (kind == "tgz" && strings.HasPrefix(tc.name, "zip")) {
				continue
			}
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				dir := t.TempDir()
				arc := zipFile(t, dir, tc.m...)
				if kind == "tgz" {
					arc = tgzFile(t, dir, tc.m...)
				}
				dest := filepath.Join(dir, "out")
				files, err := Unpack(arc, dest, 0)
				if err == nil {
					t.Fatalf("accepted: %q", files)
				}
				t.Log(err)
				if _, err := os.Stat(dest); err == nil {
					t.Fatal("staging folder left after a refused archive")
				}
				if _, err := os.Stat(filepath.Join(dir, "etc")); err == nil {
					t.Fatal("wrote outside the folder")
				}
			})
		}
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.rar"), []byte("x"), 0o644)
	if _, err := Unpack(filepath.Join(dir, "a.rar"), filepath.Join(dir, "out"), 0); err == nil {
		t.Fatal("unknown format accepted")
	}
	if Ending("x.TGZ") != ".tgz" || Ending("x.tar.gz") != ".tar.gz" || Ending("x.tar") != "" || Ending("x.exe") != "" {
		t.Fatal("Ending")
	}
}
