package ui

import (
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// A second start of the launcher finds the song folder locked: the window is open already. It asks that window to
// come to the front by writing a file into the user's data folder, which the open window watches. No network, no
// sockets. (On Wayland a program may not take the focus itself: there the desktop decides.)
const raiseFile = "show-window"

// AskToRaise is called by a second start: the open window shows itself.
func AskToRaise(userdata string) error {
	return os.WriteFile(filepath.Join(userdata, raiseFile), []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600)
}

// watchRaise brings the window to the front whenever a second start asks for it. stop ends the watch.
func (a *App) watchRaise() (stop func()) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return func() {}
	}
	if err := w.Add(a.o.UserData); err != nil {
		w.Close()
		return func() {}
	}
	go func() {
		for {
			select {
			case e, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Base(e.Name) == raiseFile && e.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					a.do(a.raise)
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() { w.Close() }
}
