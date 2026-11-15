package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/update"
)

//go:embed windows.nsi.tmpl
var nsiTemplate string

// Options are the settings of the commands that build packages.
type Options struct {
	Out         string // output folder, usually "dist"
	LauncherDir string // folder with the launcher programs; "": download the release named in bundle.json
	Makensis    string // the makensis program; "": search PATH; "-": don't build the setup program
}

const (
	windows     = "windows-amd64"
	launcherExe = "karaoke-launcher.exe"
	maxArchive  = 16 << 30
)

// Name returns the bundle's name for installers and menus: English, else the text for all languages, else any.
func (b *Bundle) Name() string {
	n := b.Config.Name
	for _, code := range append([]string{"en", ""}, slices.Sorted(maps.Keys(n))...) {
		if s := strings.TrimSpace(n[code]); s != "" {
			return s
		}
	}
	return b.Config.ID
}

// componentFolders returns the folder of every component that has an archive for platform, relative to the
// launcher's folder: components.<id>.dir.<system> must be "{appdir}/<path>". Shipped files must not be inside them,
// and they must not be inside each other.
func (b *Bundle) componentFolders(platform, system string) (map[string]string, error) {
	dirs := map[string]string{}
	for _, id := range slices.Sorted(maps.Keys(b.Components)) {
		if _, ok := b.Components[id].Files[platform]; !ok {
			continue
		}
		cc, _ := b.configComponent(id)
		raw := cc.Dir[system]
		rel, ok := strings.CutPrefix(raw, "{appdir}/")
		if !ok || !safeRel(rel) || strings.ContainsAny(rel, "{}") {
			return nil, fmt.Errorf(`component %s: "dir.%s" in launcher.json is %q; for a package it must be "{appdir}/<folder>"`, id, system, raw)
		}
		for other, orel := range dirs {
			if inside(rel, orel) || inside(orel, rel) {
				return nil, fmt.Errorf("the folders of components %s and %s overlap", other, id)
			}
		}
		for _, s := range append([]string{launcherExe}, shippedRels(b)...) {
			if inside(s, rel) {
				return nil, fmt.Errorf("%s is shipped, but it is inside the folder of component %s", s, id)
			}
		}
		dirs[id] = rel
	}
	return dirs, nil
}

func shippedRels(b *Bundle) []string {
	var l []string
	for _, s := range b.Shipped {
		l = append(l, s.Rel)
	}
	return l
}

// inside reports whether rel is folder or a path inside it (upper and lower case are the same, as on Windows).
func inside(rel, folder string) bool {
	r, f := strings.ToLower(rel), strings.ToLower(folder)
	return r == f || strings.HasPrefix(r, f+"/")
}

// componentArchive returns the archive of a component: the local file, or the download in cache.
func (b *Bundle) componentArchive(ctx context.Context, f File, cache string) (string, error) {
	if f.Path != "" {
		return b.local(f.Path)
	}
	dest := filepath.Join(cache, "components", strings.TrimPrefix(f.SHA256, "sha256:")+update.Ending(f.URL))
	if err := fetchChecked(ctx, f.URL, f.SHA256, dest, maxArchive, 0o644); err != nil {
		return "", fmt.Errorf("%s: %v", f.URL, err)
	}
	return dest, nil
}

// assemble fills dir with the launcher's folder for platform: the launcher program, the shipped files, the state
// file of the launcher, and every component with an archive for platform, unpacked into its folder with its state
// file. It returns the files of the launcher component (without state file).
func (b *Bundle) assemble(ctx context.Context, o Options, platform, system, exeName, dir string) ([]string, error) {
	folders, err := b.componentFolders(platform, system)
	if err != nil {
		return nil, err
	}
	exe, err := b.LauncherBinary(ctx, platform, o.LauncherDir, filepath.Join(o.Out, "cache"))
	if err != nil {
		return nil, err
	}
	if err := copyFile(exe, filepath.Join(dir, exeName), 0o755); err != nil {
		return nil, err
	}
	files := []string{exeName}
	for _, s := range b.Shipped {
		if err := copyFile(s.Abs, filepath.Join(dir, filepath.FromSlash(s.Rel)), 0o644); err != nil {
			return nil, err
		}
		files = append(files, s.Rel)
	}
	if err := update.WriteState(dir, update.State{Component: config.Launcher, Version: b.Version, Files: files}); err != nil {
		return nil, err
	}
	for _, id := range slices.Sorted(maps.Keys(folders)) {
		c := b.Components[id]
		archive, err := b.componentArchive(ctx, c.Files[platform], filepath.Join(o.Out, "cache"))
		if err != nil {
			return nil, fmt.Errorf("component %s: %v", id, err)
		}
		target := filepath.Join(dir, filepath.FromSlash(folders[id]))
		unpacked, err := update.Unpack(archive, target, c.Files[platform].Strip)
		if err != nil {
			return nil, fmt.Errorf("component %s: %v", id, err)
		}
		cc, _ := b.configComponent(id)
		var listed []string // as the launcher's own installation lists them: kept files are not listed
		for _, rel := range unpacked {
			if !update.Keep(cc.Keep, rel) {
				listed = append(listed, rel)
			}
		}
		if err := update.WriteState(target, update.State{Component: id, Version: c.Version, Files: listed}); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// Windows writes <Out>/windows/: the portable zip (<id>-<version>-windows-portable.zip), the launcher component for
// the update site (launcher-<version>-windows-amd64.zip), installer.nsi with karaoke-launcher.ico and its files in
// installer-files/, and the setup program when makensis is found. It returns the files written.
func (b *Bundle) Windows(ctx context.Context, o Options) ([]string, error) {
	out := filepath.Join(o.Out, "windows")
	if err := os.RemoveAll(out); err != nil {
		return nil, err
	}
	id := b.Config.ID
	stage := filepath.Join(out, "installer-files", id)
	files, err := b.assemble(ctx, o, windows, "windows", launcherExe, stage)
	if err != nil {
		return nil, err
	}
	var written []string
	portable := filepath.Join(out, fmt.Sprintf("%s-%s-windows-portable.zip", id, b.Version))
	if err := zipFolder(portable, stage, id+"/", nil); err != nil {
		return nil, err
	}
	component := filepath.Join(out, fmt.Sprintf("launcher-%s-%s.zip", b.Version, windows))
	if err := zipFolder(component, stage, "", files); err != nil {
		return nil, err
	}
	written = append(written, portable, component)

	icon, err := b.LoadIcon()
	if err != nil {
		return nil, err
	}
	var ico bytes.Buffer
	if err := WriteICO(&ico, icon); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(out, "karaoke-launcher.ico"), ico.Bytes(), 0o644); err != nil {
		return nil, err
	}
	setup := fmt.Sprintf("%s-%s-windows-setup.exe", id, b.Version)
	script, err := b.nsis(stage, out, setup)
	if err != nil {
		return nil, err
	}
	nsi := filepath.Join(out, "installer.nsi")
	if err := os.WriteFile(nsi, script, 0o644); err != nil {
		return nil, err
	}
	written = append(written, nsi)

	makensis := o.Makensis
	if makensis == "" {
		makensis, _ = exec.LookPath("makensis")
	}
	if makensis == "" || makensis == "-" {
		return written, nil
	}
	cmd := exec.CommandContext(ctx, makensis, "-V2", "-WX", "-INPUTCHARSET", "UTF8", "installer.nsi")
	cmd.Dir = out
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("makensis: %v\n%s", err, msg)
	}
	return append(written, filepath.Join(out, setup)), nil
}

// nsisEscape makes s safe inside a "…" string of an NSIS script: $ starts a variable, $\" is a quote, and line
// breaks would end the command (nsis.sourceforge.io/Docs/Chapter4.html, 4.1 and 4.2.4).
func nsisEscape(s string) string {
	return strings.NewReplacer("$", "$$", `"`, `$\"`, "\r", `$\r`, "\n", `$\n`, "\t", `$\t`).Replace(s)
}

func nsisQuote(s string) string { return `"` + nsisEscape(s) + `"` }

// shortcutName makes a file name for the start menu from the bundle's name.
func shortcutName(name, id string) string {
	s := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '-'
		}
		return r
	}, name)
	if s = strings.TrimRight(strings.TrimSpace(s), ". "); s == "" {
		return id
	}
	return s
}

type nsiDir struct {
	Target string   // quoted: "$INSTDIR\…"
	Files  []string // quoted source paths, relative to the script
}

// nsis returns the installer script for the files in stage (a folder below out).
func (b *Bundle) nsis(stage, out, setup string) ([]byte, error) {
	byDir := map[string][]string{}
	err := filepath.WalkDir(stage, func(name string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(stage, name)
		src, _ := filepath.Rel(out, name)
		dir := path.Dir(filepath.ToSlash(rel))
		byDir[dir] = append(byDir[dir], nsisQuote(filepath.ToSlash(src)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	var dirs []nsiDir
	for _, d := range slices.Sorted(maps.Keys(byDir)) {
		target := `"$INSTDIR"`
		if d != "." {
			target = `"$INSTDIR\` + nsisEscape(strings.ReplaceAll(d, "/", `\`)) + `"`
		}
		dirs = append(dirs, nsiDir{Target: target, Files: byDir[d]})
	}
	name := b.Name()
	t, err := template.New("nsi").Funcs(template.FuncMap{"q": nsisQuote}).Parse(nsiTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]any{
		"ID": b.Config.ID, "Version": b.Version, "Name": name, "NameAmp": strings.ReplaceAll(name, "&", "&&"),
		"Developer": b.Developer, "OutFile": setup, "Dirs": dirs,
		"Shortcut": `"$SMPROGRAMS\` + nsisEscape(shortcutName(name, b.Config.ID)) + `.lnk"`,
		"Removed":  name + " was removed. Your songs and settings are kept.",
	})
	return buf.Bytes(), err
}

// copyFile copies src to dst (creating its folder).
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// zipFolder writes the files below dir into a zip, each named prefix + its path with "/". only, when not nil,
// limits it to these paths.
func zipFolder(dst, dir, prefix string, only []string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	err = filepath.WalkDir(dir, func(name string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, name)
		rel = filepath.ToSlash(rel)
		if only != nil && !slices.Contains(only, rel) {
			return nil
		}
		fi, err := e.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		h.Name, h.Method = prefix+rel, zip.Deflate
		w, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(name)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
	if cerr := z.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
