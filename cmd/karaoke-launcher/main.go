// Command karaoke-launcher is a desktop launcher for karaoke communities: choose song packs, download them, start
// the game. Everything community-specific comes from launcher.json.
package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2/app"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/library"
	"github.com/basisbit/karaoke-launcher/internal/settings"
	"github.com/basisbit/karaoke-launcher/internal/ui"
)

// version is set at release time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	appdir := "."
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			appdir = filepath.Dir(exe)
		}
	}
	cfgPath := flag.String("config", filepath.Join(appdir, "launcher.json"), "launcher.json to use (for development)")
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
	vars := config.Vars{AppDir: appdir, UserData: userdata}
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
	var feeds []library.Feed
	for _, f := range cfg.Feeds {
		feeds = append(feeds, library.Feed{ID: f.ID, Address: f.Address, Key: f.Key, Starter: f.Starter})
	}
	if cfg.AllowOtherFeeds {
		for _, f := range set.Feeds {
			feeds = append(feeds, library.Feed{ID: library.AddedID(f.Address), Address: f.Address, Key: f.Key})
		}
	}
	lib, err := library.Open(userdata, set.Library, feeds, nil)
	if err != nil {
		if errors.Is(err, library.ErrRunning) {
			os.Exit(0) // the other window is open already; a second one would only confuse
		}
		fatal(set, "error.generic", err)
		return
	}
	defer lib.Close()
	lib.SetSeeds(set.Import)

	fa := app.NewWithID("io.github.basisbit.karaoke-launcher." + cfg.ID)
	ui.New(fa, ui.Options{Config: cfg, Settings: set, Library: lib, AppDir: appdir, UserData: userdata,
		Version: version, GOOS: runtime.GOOS}).Run()
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
