package ui

import (
	"context"
	"strings"
	"time"

	"fyne.io/fyne/v2/dialog"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/update"
)

// updateLine is what the update row on the home screen shows; empty text hides it. Only touched on the UI goroutine.
type updateLine struct {
	text   string // translation key
	button string // translation key, or "" for no button
	action func()
}

// doWait runs f on the UI goroutine and waits for it.
func (a *App) doWait(f func()) {
	done := make(chan struct{})
	a.do(func() { f(); close(done) })
	<-done
}

func (a *App) setUpdateLine(l updateLine) {
	a.updLine = l
	if a.home != nil {
		a.home.showUpdate()
	}
}

// updateError shows that an update failed, once per run.
func (a *App) updateError(err error) {
	if !a.updErrShown {
		a.updErrShown = true
		a.showError("error.update", err)
	}
}

// runUpdates checks for program updates (background goroutine). Other components (the game) are installed at once
// while Play is disabled, after the user closed the game; the launcher's own update waits for the user to restart.
func (a *App) runUpdates() {
	defer a.doWait(func() {
		a.checking = false
		a.closeWhenDone()
	})
	u := a.o.Updater
	ctx := context.Background()
	skip := map[string]bool{}     // installed at the next start: the user chose "Later", or installing failed
	for _, t := range u.Targets { // downloaded at an earlier run, not installed yet
		if t.ID != config.Launcher && update.Pending(t.Dir, t.ID) {
			skip[t.ID] = a.installComponent(t, func() error { return nil })
		}
	}
	res, err := u.Check(ctx)
	if err != nil {
		return // no update, a damaged or older app.json, no network: not the user's concern (docs/updates.md)
	}
	var self *update.Update
	for _, up := range res.Updates {
		if up.ID == config.Launcher {
			self = &up
			continue
		}
		if !skip[up.ID] {
			a.installComponent(up.Target, func() error { return u.Stage(ctx, up, nil) })
		}
	}
	switch {
	case self != nil:
		a.doWait(func() { a.setUpdateLine(updateLine{text: "update.downloading"}) })
		err := u.Stage(ctx, *self, nil)
		a.doWait(func() {
			if err != nil {
				a.setUpdateLine(updateLine{})
				a.updateError(err)
				return
			}
			a.setUpdateLine(updateLine{text: "update.ready", button: "update.restart", action: func() { a.restart(*self) }})
		})
	case res.Notice && a.o.Config.Updates != nil && a.o.Config.Updates.Page != "":
		page := a.o.Config.Updates.Page
		a.doWait(func() {
			a.setUpdateLine(updateLine{text: "update.notice", button: "update.get", action: func() { a.openURL(page) }})
		})
	}
}

// installComponent downloads an update of a component such as the game (stage) and installs it, with Play disabled
// and no feed sync running (a theme folder may be inside the game's folder). Before installing, the user is asked to
// close the game. It returns true when the update waits for the next start: the user chose "Later", or installing
// failed part-way. Then the game has old and new files, so Play stays disabled.
func (a *App) installComponent(t update.Target, stage func() error) (skip bool) {
	a.doWait(func() {
		a.updating = true
		a.setUpdateLine(updateLine{text: "update.game"})
		a.statusChanged(a.o.Library.Status())
	})
	err := stage()
	broken := false
	played := false
	a.doWait(func() { played = a.played })
	switch {
	case err == nil && played:
		skip = true // the game runs or was started: installed at the next start (runUpdates, as after "Later")
	case err == nil:
		a.waitForGame()
		if skip = !a.gameClosed(t.Dir); !skip {
			a.o.Library.Exclusive(func() { _, err = update.Apply(t.Dir, t.ID, t.Keep) })
			broken = err != nil && update.Pending(t.Dir, t.ID)
			skip = broken
		}
	}
	a.doWait(func() {
		a.updating = false
		a.updBroken = a.updBroken || broken
		if a.updBroken {
			a.setUpdateLine(updateLine{text: "update.broken"})
		} else {
			a.setUpdateLine(updateLine{})
		}
		a.statusChanged(a.o.Library.Status())
		if err != nil {
			a.updateError(err)
		}
	})
	return skip
}

// waitForGame returns when no game that the launcher started runs. Play stays disabled while updating, so none can
// start meanwhile.
func (a *App) waitForGame() {
	for {
		running := false
		a.doWait(func() { running = a.gameRunning })
		if !running {
			return
		}
		time.Sleep(time.Second)
	}
}

// gameClosed asks the user to close every copy of the game that may still run (also ones not started by the
// launcher) and returns true when they confirm. Where the launcher can see the running programs (Linux, Windows), it
// then checks that none runs from the game's folder, and asks again while one does. false: the user chose "Later".
func (a *App) gameClosed(dir string) bool {
	running := a.o.Running
	if running == nil {
		running = update.Running
	}
	var still []string
	for {
		answer := make(chan bool, 1)
		a.do(func() {
			text := a.tr.T("update.close.text")
			if len(still) > 0 {
				text = a.tr.T("update.close.still", "programs", strings.Join(still, ", "))
			}
			d := dialog.NewConfirm(a.tr.T("update.close.title"), text, func(ok bool) {
				a.closeAsk = nil
				answer <- ok
			}, a.win)
			d.SetConfirmText(a.tr.T("update.close.done"))
			d.SetDismissText(a.tr.T("update.close.later"))
			a.closeAsk = d
			d.Show()
		})
		if !<-answer {
			return false
		}
		names, ok := running(dir)
		if !ok || len(names) == 0 {
			return true
		}
		still = names
	}
}

// restart installs the launcher's update and starts the new version. If the user never presses the button, the
// next start installs it (main.go).
func (a *App) restart(up update.Update) {
	a.setUpdateLine(updateLine{text: "update.ready"}) // no second click
	go func() {
		a.o.Library.Pause()
		if _, err := update.Apply(up.Dir, up.ID, up.Keep); err != nil {
			a.do(func() {
				a.setUpdateLine(updateLine{})
				a.updErrShown = false
				a.updateError(err)
			})
			return
		}
		a.o.Library.Close() // the new process needs the single-instance lock
		relaunch := a.o.Relaunch
		if relaunch == nil {
			relaunch = update.Relaunch
		}
		err := relaunch()
		a.do(func() {
			if err == nil {
				a.fa.Quit()
				return
			}
			// The new version is installed, but this window has given up the library: it can't go on working.
			a.showError("update.restart.failed", err).SetOnClosed(a.fa.Quit)
		})
	}()
}
