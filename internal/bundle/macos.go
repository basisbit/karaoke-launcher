package bundle

import (
	"bytes"
	"context"
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
	if _, err := b.assemble(ctx, o, darwin, "macos", res, filepath.Join(contents, "MacOS", "karaoke-launcher")); err != nil {
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
	if signer, err = signApp(ctx, o.Signer, app); err != nil {
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

// signApp signs the app ad hoc (no certificate): with codesign on a Mac ("codesign --force --sign -", TN2206), or with
// rcodesign on other systems ("rcodesign sign", which signs ad hoc without a key; gregoryszorc.com/docs/apple-codesign).
// signer is the program: "" searches PATH for codesign, then rcodesign; "-" doesn't sign. It returns the program used.
func signApp(ctx context.Context, signer, app string) (string, error) {
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
	args := []string{"--force", "--sign", "-", app}
	if strings.Contains(filepath.Base(signer), "rcodesign") {
		args = []string{"sign", app}
	}
	if msg, err := exec.CommandContext(ctx, signer, args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s: %v\n%s", signer, err, msg)
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
