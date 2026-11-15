package bundle

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The Flatpak (docs.flatpak.org: manifests, conventions; AppStream 1.2; Desktop Entry 1.5; read 2026-11-15):
// <out>/flatpak/<app_id>.json is the flatpak-builder manifest. It lists the community's modules (the game) and then
// one module that copies files/ into the Flatpak: the launcher with its files to /app/lib/<id>/ (the launcher's
// folder), a link /app/bin/karaoke-launcher, the desktop file, the metainfo file and the icons, all named after
// the app id so that Flatpak exports them.

var iconSizes = []int{64, 128, 256, 512}

// FlatpakFiles writes <Out>/flatpak/ for flatpak-builder (the manifest and files/) and returns the manifest's path.
func (b *Bundle) FlatpakFiles(ctx context.Context, o Options) (string, error) {
	f, appID, id := b.Flatpak, b.Config.AppID, b.Config.ID
	if f == nil || appID == "" {
		return "", errors.New(`the Flatpak needs "flatpak" in bundle.json and "app_id" in launcher.json`)
	}
	out := filepath.Join(o.Out, "flatpak")
	files := filepath.Join(out, "files")
	if err := os.RemoveAll(files); err != nil { // repo/ stays: a new version is added to it
		return "", err
	}
	app := filepath.Join(files, "app")

	exe, err := b.LauncherBinary(ctx, "linux-amd64", o.LauncherDir, filepath.Join(o.Out, "cache"))
	if err != nil {
		return "", err
	}
	if err := copyFile(exe, filepath.Join(app, "karaoke-launcher"), 0o755); err != nil {
		return "", err
	}
	for _, s := range b.Shipped {
		if s.Rel == "launcher.json" {
			continue
		}
		if err := copyFile(s.Abs, filepath.Join(app, filepath.FromSlash(s.Rel)), 0o644); err != nil {
			return "", err
		}
	}
	cfg, err := flatpakConfig(filepath.Join(b.Dir, "launcher.json"))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(app, "launcher.json"), cfg, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(files, appID+".desktop"), []byte(b.desktopFile()), 0o644); err != nil {
		return "", err
	}
	meta, err := b.metainfo(time.Now())
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(files, appID+".metainfo.xml"), meta, 0o644); err != nil {
		return "", err
	}
	icon, err := b.LoadIcon()
	if err != nil {
		return "", err
	}
	install := []string{
		"mkdir -p /app/lib/" + id + " /app/bin",
		"cp -R app/. /app/lib/" + id + "/",
		"chmod 755 /app/lib/" + id + "/karaoke-launcher",
		"ln -sf /app/lib/" + id + "/karaoke-launcher /app/bin/karaoke-launcher",
		"install -Dm644 " + appID + ".desktop /app/share/applications/" + appID + ".desktop",
		"install -Dm644 " + appID + ".metainfo.xml /app/share/metainfo/" + appID + ".metainfo.xml",
	}
	for _, n := range iconSizes {
		data, err := scaledPNG(icon, n)
		if err != nil {
			return "", err
		}
		name := fmt.Sprintf("icons/%d.png", n)
		if err := os.MkdirAll(filepath.Join(files, "icons"), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(files, filepath.FromSlash(name)), data, 0o644); err != nil {
			return "", err
		}
		install = append(install, fmt.Sprintf("install -Dm644 %s /app/share/icons/hicolor/%dx%d/apps/%s.png", name, n, n, appID))
	}

	var modules []any
	for _, m := range f.Modules { // the community's modules, from where they are (their sources are relative to them)
		rel, err := filepath.Rel(out, filepath.Join(b.Dir, filepath.FromSlash(m)))
		if err != nil {
			return "", err
		}
		modules = append(modules, filepath.ToSlash(rel))
	}
	modules = append(modules, map[string]any{
		"name": "karaoke-launcher", "buildsystem": "simple", "build-commands": install,
		"sources": []any{map[string]any{"type": "dir", "path": "files"}},
	})
	manifest := map[string]any{
		"id": appID, "runtime": f.Runtime, "runtime-version": f.RuntimeVersion, "sdk": f.Sdk,
		"command": "karaoke-launcher", "finish-args": f.FinishArgs, "modules": modules,
	}
	if f.FinishArgs == nil {
		manifest["finish-args"] = []string{}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(out, appID+".json")
	return path, os.WriteFile(path, append(data, '\n'), 0o644)
}

// flatpakConfig returns launcher.json without "updates" and "components": Flatpak updates the programs itself.
func flatpakConfig(file string) ([]byte, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var c map[string]json.RawMessage
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	delete(c, "updates")
	delete(c, "components")
	out, err := json.MarshalIndent(c, "", "  ")
	return append(out, '\n'), err
}

// desktopValue escapes a value of a desktop file (Desktop Entry 1.5: \s \n \t \r \\).
func desktopValue(s string) string {
	s = strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(s)
	if strings.HasPrefix(s, " ") {
		s = `\s` + s[1:]
	}
	return s
}

func (b *Bundle) desktopFile() string {
	var buf strings.Builder
	buf.WriteString("[Desktop Entry]\nType=Application\n")
	buf.WriteString("Name=" + desktopValue(b.Name()) + "\n")
	for _, code := range slices.Sorted(maps.Keys(b.Config.Name)) {
		if code != "" && code != "en" && strings.TrimSpace(b.Config.Name[code]) != "" {
			buf.WriteString("Name[" + code + "]=" + desktopValue(b.Config.Name[code]) + "\n")
		}
	}
	buf.WriteString("Comment=" + desktopValue(b.Summary) + "\n")
	buf.WriteString("Exec=karaoke-launcher\nIcon=" + b.Config.AppID + "\nTerminal=false\nCategories=AudioVideo;Audio;\n")
	return buf.String()
}

func xmlText(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// metainfo returns the AppStream file (AppStream 1.2: type desktop-application; id, name, summary, description,
// metadata_license and launchable are required).
func (b *Bundle) metainfo(now time.Time) ([]byte, error) {
	appID := b.Config.AppID
	var desc strings.Builder
	for _, para := range strings.Split(strings.ReplaceAll(b.Description, "\r\n", "\n"), "\n\n") {
		if para = strings.TrimSpace(para); para != "" {
			desc.WriteString("    <p>" + xmlText(strings.Join(strings.Fields(para), " ")) + "</p>\n")
		}
	}
	devID := appID[:strings.LastIndex(appID, ".")] // the community's reverse-DNS name
	text := `<?xml version="1.0" encoding="UTF-8"?>
<component type="desktop-application">
  <id>` + xmlText(appID) + `</id>
  <metadata_license>CC0-1.0</metadata_license>
  <project_license>` + xmlText(b.License) + `</project_license>
  <name>` + xmlText(b.Name()) + `</name>
  <summary>` + xmlText(b.Summary) + `</summary>
  <description>
` + desc.String() + `  </description>
  <developer id="` + xmlText(devID) + `">
    <name>` + xmlText(b.Developer) + `</name>
  </developer>
  <launchable type="desktop-id">` + xmlText(appID) + `.desktop</launchable>
  <url type="homepage">` + xmlText(b.Homepage) + `</url>
  <content_rating type="oars-1.1"/>
  <releases>
    <release version="` + xmlText(b.Version) + `" date="` + now.UTC().Format("2006-01-02") + `"/>
  </releases>
</component>
`
	return []byte(text), nil
}

// Flathub's repository file: the runtimes come from there (docs.flatpak.org, single-file-bundles, repositories).
const flathubRepo = "https://dl.flathub.org/repo/flathub.flatpakrepo"

// FlatpakBranch is the branch the Flatpak is built on; the .flatpakref names it.
const FlatpakBranch = "stable"

// BuildFlatpak runs flatpak-builder (with --user, runtimes from Flathub) on the files of FlatpakFiles. The result
// goes into <Out>/flatpak/repo/, a static repository that the site publishes at <site>flatpak/repo/; earlier versions
// stay in it. Then it writes <app_id>.flatpak (one file with the app; the runtime comes from Flathub) and
// <app_id>.flatpakref. log receives the tools' output.
func (b *Bundle) BuildFlatpak(ctx context.Context, o Options, log io.Writer) ([]string, error) {
	manifest, err := b.FlatpakFiles(ctx, o)
	if err != nil {
		return nil, err
	}
	out, appID := filepath.Dir(manifest), b.Config.AppID
	run := func(name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = out, log, log
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s %s: %v", name, strings.Join(args, " "), err)
		}
		return nil
	}
	repoURL := b.Site + "flatpak/repo/"
	steps := [][]string{
		{"flatpak", "--user", "remote-add", "--if-not-exists", "flathub", flathubRepo},
		{"flatpak-builder", "--user", "--install-deps-from=flathub", "--disable-rofiles-fuse", "--force-clean",
			"--state-dir=" + filepath.Join("..", "cache", "flatpak-builder"), "--repo=repo",
			"--default-branch=" + FlatpakBranch, "build", filepath.Base(manifest)},
		{"flatpak", "build-update-repo", "--generate-static-deltas", "--prune", "--title=" + b.Name(), "repo"},
		{"flatpak", "build-bundle", "--runtime-repo=" + flathubRepo, "--repo-url=" + repoURL, "repo", appID + ".flatpak", appID, FlatpakBranch},
	}
	for _, s := range steps {
		if err := run(s[0], s[1:]...); err != nil {
			return nil, err
		}
	}
	ref := "[Flatpak Ref]\nName=" + appID + "\nBranch=" + FlatpakBranch + "\nUrl=" + repoURL +
		"\nTitle=" + oneLine(b.Name()) + "\nHomepage=" + b.Homepage + "\nRuntimeRepo=" + flathubRepo +
		"\nIsRuntime=false\nSuggestRemoteName=" + b.Config.ID + "\n"
	if err := os.WriteFile(filepath.Join(out, appID+".flatpakref"), []byte(ref), 0o644); err != nil {
		return nil, err
	}
	return []string{filepath.Join(out, "repo"), filepath.Join(out, appID+".flatpak"), filepath.Join(out, appID+".flatpakref")}, nil
}

// oneLine keeps a text on one line of a key file.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
