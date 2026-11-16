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
	Signer      string // the program that signs the macOS app (codesign or rcodesign); "": search PATH; "-": don't sign
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

// componentFolders returns the folder of every component that the package for platform contains, relative to the
// launcher's folder: the components with an archive for platform whose components.<id>.dir.<system> is
// "{appdir}/<path>". Components in other folders ({userdata}, {home}) are left out: the launcher installs them at its
// first start. With inApp false (macOS: the app must not change itself), a component in {appdir} is an error.
// Shipped files must not be inside the folders, and they must not be inside each other.
func (b *Bundle) componentFolders(platform, system string, inApp bool) (map[string]string, error) {
	dirs := map[string]string{}
	for _, id := range slices.Sorted(maps.Keys(b.Components)) {
		if _, ok := b.Components[id].Files[platform]; !ok {
			continue
		}
		cc, _ := b.configComponent(id)
		raw := cc.Dir[system]
		if raw == "" {
			return nil, fmt.Errorf(`component %s has an archive for %s, but no "dir.%s" in launcher.json`, id, platform, system)
		}
		if raw != "{appdir}" && !strings.HasPrefix(raw, "{appdir}/") {
			continue // installed by the launcher at its first start
		}
		if !inApp {
			return nil, fmt.Errorf(`component %s: "dir.%s" in launcher.json is %q; on macOS it must be outside the app, for example "{userdata}/%s": the app must not change itself`, id, system, raw, id)
		}
		rel, ok := strings.CutPrefix(raw, "{appdir}/")
		if !ok || !safeRel(rel) || strings.ContainsAny(rel, "{}") {
			return nil, fmt.Errorf(`component %s: "dir.%s" in launcher.json is %q; in the launcher's folder it must be "{appdir}/<folder>"`, id, system, raw)
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

// assemble fills dir with the launcher's folder for platform: the shipped files, the state file of the launcher, and
// every component the package contains (componentFolders), unpacked into its folder with its state file. The launcher
// program goes to exe; it belongs to the launcher component when it is inside dir. It returns the files of the
// launcher component (without state file).
func (b *Bundle) assemble(ctx context.Context, o Options, platform, system, dir, exe string) (files, kept []string, err error) {
	folders, err := b.componentFolders(platform, system, system != "macos")
	if err != nil {
		return nil, nil, err
	}
	src, err := b.LauncherBinary(ctx, platform, o.LauncherDir, filepath.Join(o.Out, "cache"))
	if err != nil {
		return nil, nil, err
	}
	if err := copyFile(src, exe, 0o755); err != nil {
		return nil, nil, err
	}
	if rel, err := filepath.Rel(dir, exe); err == nil && filepath.IsLocal(rel) {
		files = append(files, filepath.ToSlash(rel))
	}
	for _, s := range b.Shipped {
		if err := s.copyTo(filepath.Join(dir, filepath.FromSlash(s.Rel))); err != nil {
			return nil, nil, err
		}
		files = append(files, s.Rel)
	}
	if err := update.WriteState(dir, update.State{Component: config.Launcher, Version: b.Version, Files: files}); err != nil {
		return nil, nil, err
	}
	for _, id := range slices.Sorted(maps.Keys(folders)) {
		c := b.Components[id]
		archive, err := b.componentArchive(ctx, c.Files[platform], filepath.Join(o.Out, "cache"))
		if err != nil {
			return nil, nil, fmt.Errorf("component %s: %v", id, err)
		}
		target := filepath.Join(dir, filepath.FromSlash(folders[id]))
		unpacked, err := update.Unpack(archive, target, c.Files[platform].Strip)
		if err != nil {
			return nil, nil, fmt.Errorf("component %s: %v", id, err)
		}
		cc, _ := b.configComponent(id)
		var listed []string // as the launcher's own installation lists them: kept files are not listed
		for _, rel := range unpacked {
			if !update.Keep(cc.Keep, rel) {
				listed = append(listed, rel)
			} else if k, err := filepath.Rel(dir, filepath.Join(target, filepath.FromSlash(rel))); err == nil {
				kept = append(kept, filepath.ToSlash(k))
			}
		}
		if err := update.WriteState(target, update.State{Component: id, Version: c.Version, Files: listed}); err != nil {
			return nil, nil, err
		}
	}
	return files, kept, nil
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
	files, kept, err := b.assemble(ctx, o, windows, "windows", stage, filepath.Join(stage, launcherExe))
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
	script, err := b.nsis(stage, out, setup, kept)
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
	Target string // quoted: "$INSTDIR\…"
	Files  []nsiFile
}

type nsiFile struct {
	Src  string // quoted source path, relative to the script
	Keep bool   // a kept file (the game's settings): installed only when it is not there yet
}

// nsis returns the installer script for the files in stage (a folder below out). kept are paths relative to stage.
func (b *Bundle) nsis(stage, out, setup string, kept []string) ([]byte, error) {
	byDir := map[string][]nsiFile{}
	err := filepath.WalkDir(stage, func(name string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(stage, name)
		src, _ := filepath.Rel(out, name)
		dir := path.Dir(filepath.ToSlash(rel))
		byDir[dir] = append(byDir[dir], nsiFile{nsisQuote(filepath.ToSlash(src)), slices.Contains(kept, filepath.ToSlash(rel))})
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
		"Removed":  name + " was removed. Your songs, settings and scores are kept.",
		"NotAll":   "Some files of " + name + " could not be removed. Close " + name + " and the game, then uninstall again.",
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
		if e.Type()&fs.ModeSymlink != 0 { // a link stays a link (the folders of a macOS app have some): its target is the content
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			h.Method = zip.Store
			w, err := z.CreateHeader(h)
			if err == nil {
				_, err = io.WriteString(w, filepath.ToSlash(target))
			}
			return err
		}
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
