package bundle

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The macOS app (Apple: "Bundle Programming Guide", Anatomy of a macOS Application Bundle; "Information Property
// List Key Reference", Core Foundation Keys and Launch Services Keys; TN2206 macOS Code Signing In Depth; fyne-io/tools
// Info.plist template; read 2026-11-15):
// <Name>.app/Contents/Info.plist, Contents/MacOS/karaoke-launcher (only programs may be there), Contents/Resources/
// with icon.icns and the launcher's folder: launcher.json, the shipped files and its state file (config.AppDir).
// The app never changes itself: the launcher does not update itself on macOS (app.json has no file for darwin-arm64,
// so it shows "A new version is available" with updates.page), and components are installed outside the app at the
// first start. So an ad-hoc signature stays valid, and the read-only folder macOS may start a downloaded app from
// (app translocation) does not matter.
// Apps of "macos.apps" (a game's own app) go to Contents/Helpers/ ("Helper apps and tools", TN2206 table 3). They
// come in .dmg files, which only macOS opens (hdiutil). The app is signed inside out (TN2206: "signing the outermost
// bundle last"): first each helper app, then the launcher's app.

const darwin = "darwin-arm64"

// minMacOS is the oldest macOS that programs of the current Go release run on (go.dev/doc/go1.27: macOS 13 Ventura).
const minMacOS = "13.0"

// Macos writes <Out>/macos/: <id>-<version>-macos-arm64.zip with <Name>.app inside, and the same as a .dmg when
// hdiutil is found (only on a Mac). The app is signed ad hoc (no certificate) with codesign or rcodesign
// (Options.Signer); signer is the program used, "" when the app is not signed. It returns the files written.
func (b *Bundle) Macos(ctx context.Context, o Options) (files []string, signer string, err error) {
	if runtime.GOOS == "windows" {
		return nil, "", errNoExecBit
	}
	if b.Config.AppID == "" {
		return nil, "", fmt.Errorf(`the macOS app needs "app_id" in launcher.json (its bundle identifier)`)
	}
	out := filepath.Join(o.Out, "macos")
	if err := os.RemoveAll(out); err != nil {
		return nil, "", err
	}
	stage := filepath.Join(out, "app")
	appName := shortcutName(b.Name(), b.Config.ID) + ".app"
	contents := filepath.Join(stage, appName, "Contents")
	res := filepath.Join(contents, "Resources")
	if _, _, err := b.assemble(ctx, o, darwin, "macos", res, filepath.Join(contents, "MacOS", "karaoke-launcher")); err != nil {
		return nil, "", err
	}
	nested, err := b.helperApps(ctx, o, out, filepath.Join(contents, "Helpers"))
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), b.infoPlist(), 0o644); err != nil {
		return nil, "", err
	}
	icon, err := b.LoadIcon()
	if err != nil {
		return nil, "", err
	}
	var icns bytes.Buffer
	if err := WriteICNS(&icns, icon); err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(filepath.Join(res, "icon.icns"), icns.Bytes(), 0o644); err != nil {
		return nil, "", err
	}
	app := filepath.Join(stage, appName)
	if signer, err = signApp(ctx, o.Signer, append(nested, app)...); err != nil {
		return nil, "", err
	}

	base := fmt.Sprintf("%s-%s-macos-arm64", b.Config.ID, b.Version)
	zipFile := filepath.Join(out, base+".zip")
	if err := zipFolder(zipFile, app, appName+"/", nil); err != nil { // keeps the modes
		return nil, "", err
	}
	written := []string{zipFile}
	hdiutil, _ := exec.LookPath("hdiutil")
	if hdiutil == "" {
		return written, signer, nil
	}
	dmg := filepath.Join(out, base+".dmg")
	cmd := exec.CommandContext(ctx, hdiutil, "create", "-volname", b.Name(), "-srcfolder", stage, "-format", "UDZO", "-ov", dmg)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("hdiutil: %v\n%s", err, msg)
	}
	return append(written, dmg), signer, nil
}

// helperApps copies the apps of "macos.apps" from their .dmg files into helpers and returns their folders.
func (b *Bundle) helperApps(ctx context.Context, o Options, out, helpers string) ([]string, error) {
	if b.Mac == nil || len(b.Mac.Apps) == 0 {
		return nil, nil
	}
	hdiutil, err := exec.LookPath("hdiutil")
	if err != nil {
		return nil, fmt.Errorf("%s comes in a .dmg file, which only macOS can open (hdiutil): make the macOS app on a Mac, for example in the macos job of the workflow (docs/publishing.md)", b.Mac.Apps[0].App)
	}
	var dirs []string
	for _, a := range b.Mac.Apps {
		dmg := filepath.Join(o.Out, "cache", "apps", strings.TrimPrefix(a.SHA256, "sha256:")+".dmg")
		if err := fetchChecked(ctx, a.URL, a.SHA256, dmg, maxArchive, 0o644); err != nil {
			return nil, fmt.Errorf("%s: %v", a.URL, err)
		}
		dst := filepath.Join(helpers, a.App)
		if err := copyFromDMG(ctx, hdiutil, dmg, a.App, dst, out); err != nil {
			return nil, fmt.Errorf("%s: %v", a.URL, err)
		}
		dirs = append(dirs, dst)
	}
	return dirs, nil
}

// copyFromDMG mounts dmg (read-only, not shown in the Finder) in a new folder in tmp, copies the app name in it to dst
// with ditto (which keeps links, modes and the signature) and unmounts it.
func copyFromDMG(ctx context.Context, hdiutil, dmg, name, dst, tmp string) error {
	mnt, err := os.MkdirTemp(tmp, "mount-")
	if err != nil {
		return err
	}
	defer os.Remove(mnt)
	if msg, err := exec.CommandContext(ctx, hdiutil, "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg).CombinedOutput(); err != nil {
		return fmt.Errorf("hdiutil attach: %v\n%s", err, msg)
	}
	defer exec.Command(hdiutil, "detach", "-force", mnt).Run()
	src := filepath.Join(mnt, name)
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		entries, _ := os.ReadDir(mnt)
		var have []string
		for _, e := range entries {
			have = append(have, e.Name())
		}
		return fmt.Errorf("the .dmg has no %s; it has: %s", name, strings.Join(have, ", "))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if msg, err := exec.CommandContext(ctx, "ditto", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("ditto: %v\n%s", err, msg)
	}
	return nil
}

// signApp signs apps ad hoc (no certificate), in the order given (nested apps first): with codesign on a Mac
// ("codesign --force --sign -", TN2206), or with rcodesign on other systems ("rcodesign sign", which signs ad hoc
// without a key; gregoryszorc.com/docs/apple-codesign). signer is the program: "" searches PATH for codesign, then
// rcodesign; "-" doesn't sign. It returns the program used.
func signApp(ctx context.Context, signer string, apps ...string) (string, error) {
	if signer == "-" {
		return "", nil
	}
	if signer == "" {
		for _, name := range []string{"codesign", "rcodesign"} {
			if p, err := exec.LookPath(name); err == nil {
				signer = p
				break
			}
		}
		if signer == "" {
			return "", nil
		}
	}
	for _, app := range apps {
		args := []string{"--force", "--sign", "-", app}
		if strings.Contains(filepath.Base(signer), "rcodesign") {
			args = []string{"sign", app}
		}
		if msg, err := exec.CommandContext(ctx, signer, args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s: %v\n%s", signer, err, msg)
		}
	}
	return signer, nil
}

// errNoExecBit: Windows doesn't keep the executable bit of files, so the programs in the zip would not start on a Mac.
var errNoExecBit = errors.New("make the macOS app on Linux or macOS: Windows loses the executable bit that its programs need")

// bundleID is app_id as a macOS bundle identifier, which allows only A-Z, a-z, 0-9, "-" and ".": "_" becomes "-".
func bundleID(appID string) string { return strings.ReplaceAll(appID, "_", "-") }

// macVersion is the version as macOS wants it (period-separated integers): the leading digits and periods of
// version, for example "1.2.0" of "1.2.0-beta"; "0" when it doesn't start with a digit.
func macVersion(version string) string {
	end := strings.IndexFunc(version, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if end < 0 {
		end = len(version)
	}
	if v := strings.Trim(version[:end], "."); v != "" {
		return v
	}
	return "0"
}

func xmlText(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func (b *Bundle) infoPlist() []byte {
	name, v := xmlText(b.Name()), macVersion(b.Version)
	keys := [][2]string{
		{"CFBundleDevelopmentRegion", "<string>en</string>"},
		{"CFBundleDisplayName", "<string>" + name + "</string>"},
		{"CFBundleExecutable", "<string>karaoke-launcher</string>"},
		{"CFBundleIconFile", "<string>icon.icns</string>"},
		{"CFBundleIdentifier", "<string>" + xmlText(bundleID(b.Config.AppID)) + "</string>"},
		{"CFBundleInfoDictionaryVersion", "<string>6.0</string>"},
		{"CFBundleName", "<string>" + name + "</string>"},
		{"CFBundlePackageType", "<string>APPL</string>"},
		{"CFBundleShortVersionString", "<string>" + v + "</string>"},
		{"CFBundleSupportedPlatforms", "<array><string>MacOSX</string></array>"},
		{"CFBundleVersion", "<string>" + v + "</string>"},
		{"LSMinimumSystemVersion", "<string>" + minMacOS + "</string>"},
		{"NSHighResolutionCapable", "<true/>"},
		{"NSSupportsAutomaticGraphicsSwitching", "<true/>"},
	}
	var buf strings.Builder
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, k := range keys {
		buf.WriteString("  <key>" + k[0] + "</key>\n  " + k[1] + "\n")
	}
	buf.WriteString("</dict>\n</plist>\n")
	return []byte(buf.String())
}
