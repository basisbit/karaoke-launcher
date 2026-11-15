//go:build ignore

// gen_licenses writes licenses.txt: the licence texts of Go and of every module that is linked into the launcher on
// Linux, Windows or macOS, including licence files in sub-folders (fonts, ported code). Run with go generate.
// It fails when a module has no licence file, so that nobody ships one without noticing.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var licenceRe = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|copyright|unlicense|patents)([._-].*)?$`)

// sourceExt are files whose names match licenceRe but are code, not licences (copyright_test.go).
var sourceExt = map[string]bool{".go": true, ".c": true, ".h": true, ".s": true, ".py": true, ".sh": true}

type module struct{ path, version, dir string }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen_licenses:", err)
		os.Exit(1)
	}
}

func goCmd(env []string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

func run() error {
	mods := map[string]module{}
	for _, goos := range []string{"linux", "windows", "darwin"} {
		out, err := goCmd([]string{"GOOS=" + goos, "CGO_ENABLED=1"}, "list", "-deps", "-f",
			`{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}{{end}}`,
			"../../cmd/karaoke-launcher")
		if err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if p := strings.Split(line, "|"); len(p) == 3 {
				mods[p[0]] = module{p[0], p[1], p[2]}
			}
		}
	}
	modcache, err := goCmd(nil, "env", "GOMODCACHE")
	if err != nil {
		return err
	}
	modcache = strings.TrimSpace(modcache)
	goroot, err := goCmd(nil, "env", "GOROOT")
	if err != nil {
		return err
	}

	var b bytes.Buffer
	b.WriteString("Licences of the software this program is built from.\n")
	if err := section(&b, "Go (standard library and runtime)", strings.TrimSpace(goroot), false); err != nil {
		return err
	}
	paths := make([]string, 0, len(mods))
	for p := range mods {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		m := mods[p]
		local := !strings.HasPrefix(m.dir, modcache) // a replace to a local checkout
		version := m.version
		if local {
			version = "local checkout"
		}
		if err := section(&b, m.path+" "+version, m.dir, local); err != nil {
			return err
		}
	}
	return os.WriteFile("licenses.txt", b.Bytes(), 0o644)
}

// section appends every licence file below dir. For a local checkout of a module in a sub-folder of its repository,
// the repository's licence is taken from a parent folder (the module download includes it the same way).
func section(b *bytes.Buffer, title, dir string, local bool) error {
	var files []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "vendor" || (strings.HasPrefix(d.Name(), ".") && p != dir)) {
			return filepath.SkipDir
		}
		if !d.IsDir() && licenceRe.MatchString(d.Name()) && !sourceExt[filepath.Ext(d.Name())] {
			files = append(files, p)
		}
		return nil
	})
	for parent := filepath.Dir(dir); local && len(files) == 0 && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		entries, _ := os.ReadDir(parent)
		for _, e := range entries {
			if !e.IsDir() && licenceRe.MatchString(e.Name()) {
				files = append(files, filepath.Join(parent, e.Name()))
			}
		}
		if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil {
			break // the repository root
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("%s: no licence file in %s", title, dir)
	}
	sort.Strings(files)
	fmt.Fprintf(b, "\n%s\n%s\n", strings.Repeat("=", 60), title)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, f)
		fmt.Fprintf(b, "%s\n--- %s ---\n\n%s\n", strings.Repeat("-", 60), filepath.ToSlash(rel), strings.TrimSpace(string(data)))
	}
	return nil
}
