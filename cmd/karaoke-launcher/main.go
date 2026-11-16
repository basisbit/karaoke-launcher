// Command karaoke-launcher is a desktop launcher for karaoke communities: choose song packs, download them, start
// the game. Everything community-specific comes from launcher.json.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2/app"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/library"
	"github.com/basisbit/karaoke-launcher/internal/settings"
	"github.com/basisbit/karaoke-launcher/internal/ui"
	"github.com/basisbit/karaoke-launcher/internal/update"
)

// version is set at release time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	appdir := "."
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			appdir = config.AppDir(exe, runtime.GOOS)
		}
	}
	cfgPath := flag.String("config", filepath.Join(appdir, "launcher.json"), "launcher.json to use (for development)")
	uninstall := flag.Bool("uninstall", false, "remove the installed programs and keep everything else (run by the Windows uninstaller)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal(nil, "app.error.config", err)
		return
	}
	userdata, err := cfg.UserDataDir()
	if err != nil {
		fatal(nil, "app.error.config", err)
		return
	}
	home, _ := os.UserHomeDir()
	vars := config.Vars{AppDir: appdir, UserData: userdata, Home: home}
	if *uninstall {
		os.Exit(uninstallAll(cfg, vars))
	}
	defaultLib, err := cfg.LibraryDefault(vars)
	if err != nil {
		fatal(nil, "app.error.config", err)
		return
	}
	set, err := settings.Load(userdata, defaultLib)
	if err != nil {
		fatal(nil, "error.generic", err)
		return
	}
	lib, err := library.Open(userdata, set.Library, ui.LibraryFeeds(cfg, set), nil)
	if err != nil {
		if errors.Is(err, library.ErrRunning) {
			ui.AskToRaise(userdata) // the other window is open already: it comes to the front
			os.Exit(0)
		}
		fatal(set, "error.generic", err)
		return
	}
	defer lib.Close()
	lib.SetSeeds(set.Import)
	vars.Library = set.Library
	lib.SetTargets(cfg.Targets(runtime.GOOS, vars)) // theme folders and the like; only feeds with a key use them

	// Program updates. Only the process that holds the library lock installs them.
	targets := []update.Target{{ID: config.Launcher, Dir: appdir}}
	for id, c := range cfg.Components {
		if dir, ok, err := cfg.ComponentDir(id, runtime.GOOS, vars); ok && err == nil {
			targets = append(targets, update.Target{ID: id, Dir: dir, Keep: c.Keep})
		}
	}
	if applyStaged(targets) {
		lib.Close()
		if err := update.Relaunch(); err != nil {
			fatal(set, "error.generic", err) // the new version is installed; starting it by hand works
		}
		return
	}
	var updater *update.Updater
	if u := cfg.Updates; u != nil {
		updater = &update.Updater{URL: u.URL, Key: u.Key, ID: cfg.ID, UserData: userdata, Targets: targets}
	}

	id := "io.github.basisbit.karaoke-launcher." + cfg.ID
	if cfg.AppID != "" {
		id = cfg.AppID
	}
	fa := app.NewWithID(id)
	ui.New(fa, ui.Options{Config: cfg, Settings: set, Library: lib, AppDir: appdir, UserData: userdata,
		Version: version, GOOS: runtime.GOOS, Updater: updater}).Run()
}

// applyStaged removes files that earlier updates moved aside and installs the launcher's own update if it was
// downloaded but not installed yet (the user closed the launcher instead of restarting). It reports whether the
// launcher was updated. Errors are ignored: the old version runs, and the next start tries again. Waiting updates of
// other components (the game) are installed by the window, after the user closed the game (ui.runUpdates).
func applyStaged(targets []update.Target) (launcher bool) {
	for _, t := range targets {
		update.Cleanup(t.Dir)
	}
	ok, err := update.Apply(targets[0].Dir, targets[0].ID, targets[0].Keep)
	return ok && err == nil
}

// fatal shows one error window and returns when it is closed.
func fatal(set *settings.Settings, key string, err error) {
	lang := ""
	if set != nil {
		lang = set.Language
	}
	tr := ui.NewTranslator(lang)
	ui.ShowFatal(app.New(), tr, tr.T(key), err)
}

// uninstallAll removes what the launcher and its updates installed: every component's files, then the launcher's,
// except the running program (the uninstaller deletes it afterwards). Settings, scores and other files a game wrote,
// and the songs, stay. Exit code 1 when a file could not be removed (a program still runs); nothing is shown, the
// uninstaller tells the user.
func uninstallAll(cfg *config.Config, v config.Vars) int {
	var errs []error
	for id := range cfg.Components {
		if dir, ok, err := cfg.ComponentDir(id, runtime.GOOS, v); ok && err == nil {
			errs = append(errs, update.Uninstall(dir, id, ""))
		}
	}
	if errors.Join(errs...) == nil { // else the launcher's state file stays: the uninstaller checks it when run again
		exe, _ := os.Executable()
		errs = append(errs, update.Uninstall(v.AppDir, config.Launcher, exe))
	}
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
