package ui

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	kfeed "github.com/basisbit/karaoke-feeds/tool"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/folders"
	"github.com/basisbit/karaoke-launcher/internal/library"
	"github.com/basisbit/karaoke-launcher/internal/settings"
	"github.com/basisbit/karaoke-launcher/internal/update"
)

// Options is everything the window needs.
type Options struct {
	Config   *config.Config
	Settings *settings.Settings
	Library  *library.Library
	AppDir   string          // folder of the launcher executable
	UserData string          // per-user data folder
	Version  string          // launcher version
	GOOS     string          // runtime.GOOS; tests may set another system
	Updater  *update.Updater // program updates; nil: none
	Relaunch func() error    // starts the new launcher after its update (nil: update.Relaunch; tests replace it)
	Quit     func()          // ends the launcher after the game started (nil: the Fyne app's Quit; tests replace it)
	// Running lists the programs that run from a folder (nil: update.Running; tests replace it).
	Running func(dir string) (names []string, ok bool)
	// Do runs a function on the UI goroutine (nil: fyne.Do). Tests pass a serial runner, because Fyne's test
	// driver runs fyne.Do on the calling goroutine.
	Do func(func())
}

// App is the launcher window.
type App struct {
	o   Options
	fa  fyne.App
	win fyne.Window
	tr  *Translator
	th  *Theme

	home  *home  // the screen on show, or nil
	songs *songs // the screen on show, or nil

	infoMu sync.Mutex
	infos  map[string]*kfeed.Info // last description of each feed

	do func(func()) // run on the UI goroutine

	watcher    *resizeWatcher
	fullDialog interface{ Resize(fyne.Size) } // a dialog that follows the window size, or nil
	newsText   string                         // news, once downloaded

	gameRunning bool // only touched on the Fyne goroutine
	played      bool // the game was started: the launcher closes when no download or update runs (closeWhenDone)
	checking    bool // runUpdates runs: checking for, downloading or installing program updates
	starting    bool // the feeds are checked before the chosen packs are updated (Start)
	quitting    bool

	updating    bool       // a program update is installed: Play is disabled
	updLine     updateLine // the update row on the home screen
	updErrShown bool
	updBroken   bool                  // a program update failed part-way: Play stays disabled until the next start finishes it
	closeAsk    *dialog.ConfirmDialog // the question to close the game before its update, while it is shown
}

// New builds the window. The library's status changes are shown from now on.
func New(fa fyne.App, o Options) *App {
	a := &App{o: o, fa: fa, tr: NewTranslator(o.Settings.Language), th: NewTheme(o.Config), infos: map[string]*kfeed.Info{}}
	a.do = o.Do
	if a.do == nil {
		a.do = fyne.Do
	}
	fa.Settings().SetTheme(a.th)
	a.win = fa.NewWindow(o.Config.Name.Get(a.tr.Code()))
	if p := o.Config.Asset(o.Config.Look.Logo); p != "" { // the community's logo is also the window and app icon
		if icon, err := fyne.LoadResourceFromPath(p); err == nil {
			fa.SetIcon(icon)
			a.win.SetIcon(icon)
		}
	}
	a.win.Resize(fyne.NewSize(620, 680))
	a.showHome()
	o.Library.SetOnChange(func(s library.Status) { a.do(func() { a.statusChanged(s) }) })
	return a
}

// Window returns the main window.
func (a *App) Window() fyne.Window { return a.win }

// Run shows the window and starts the background work; it returns when the window is closed.
func (a *App) Run() {
	a.Start()
	a.win.ShowAndRun()
}

// Start begins the background work: news, checking every feed, and updating the packs already chosen.
// Nothing is downloaded for packs the user did not choose.
func (a *App) Start() {
	a.win.SetCloseIntercept(a.onClose)
	a.setReadmes()
	if a.o.Updater != nil {
		a.checking = true
		go a.runUpdates()
	}
	a.starting = true
	go func() {
		for _, f := range a.o.Library.Feeds() {
			a.inspect(f.ID) // remembered for the songs screen; errors show there
		}
		a.do(func() {
			if a.o.Library.HasSongs() {
				if err := a.o.Library.Start(nil); err != nil && !errors.Is(err, library.ErrBusy) {
					a.showError("error.generic", err)
				}
			}
			a.starting = false
			a.closeWhenDone()
		})
	}()
	if n := a.o.Config.News; n != nil {
		go func() {
			text, err := fetchNews(context.Background(), n.URL)
			if err == nil && text != "" {
				a.do(func() {
					a.newsText = text
					if a.home != nil {
						a.home.setNews(text)
					}
				})
			}
		}()
	}
}

func (a *App) vars() config.Vars {
	home, _ := os.UserHomeDir()
	return config.Vars{Library: a.o.Settings.Library, AppDir: a.o.AppDir, UserData: a.o.UserData, Home: home}
}

func (a *App) showHome() {
	a.songs = nil
	a.home = newHome(a)
	a.setContent(a.home.root)
	a.home.refresh(a.o.Library.Status())
}

func (a *App) statusChanged(s library.Status) {
	if a.home != nil {
		a.home.refresh(s)
	}
	if a.songs != nil {
		a.songs.refresh(s)
	}
	a.closeWhenDone()
}

// closeWhenDone ends the launcher once the game was started and no download and no program update runs (UI
// goroutine). A download that failed counts as finished: the next start tries again.
func (a *App) closeWhenDone() {
	if !a.played || a.quitting || a.starting || a.checking || a.updating || a.o.Library.Status().Running {
		return
	}
	a.quitting = true
	quit := a.o.Quit
	if quit == nil {
		quit = a.fa.Quit
	}
	quit()
}

func (a *App) openURL(raw string) {
	if u, err := url.Parse(raw); err == nil {
		a.fa.OpenURL(u)
	}
}

// onClose asks before a running download is stopped by closing the window.
func (a *App) onClose() {
	if !a.o.Library.Status().Running {
		a.win.Close()
		return
	}
	tr := a.tr
	d := dialog.NewConfirm(tr.T("close.title"), tr.T("close.text"), func(yes bool) {
		if yes {
			go func() {
				a.o.Library.Pause() // the library stays consistent; the next start continues
				a.do(a.win.Close)
			}()
		}
	}, a.win)
	d.SetConfirmText(tr.T("close.yes"))
	d.SetDismissText(tr.T("button.cancel"))
	d.Show()
}

// showStatusDetails shows the problem of the last download: the sentence of the status line and the technical text.
func (a *App) showStatusDetails() {
	st := a.o.Library.Status()
	if d := statusDetails(st); d != "" {
		key := problemKey(st)
		if key == "" {
			key = "status.problem"
		}
		a.showError(key, errors.New(d))
	}
}

// statusDetails is the technical text behind a problem in the status line, or "".
func statusDetails(s library.Status) string {
	var lines []string
	if s.Err != nil {
		lines = append(lines, s.Err.Error())
	}
	for id, r := range s.Results {
		for _, f := range r.Failed {
			lines = append(lines, id+": "+f)
		}
	}
	return strings.Join(lines, "\n")
}

// showError shows one sentence in plain words and a "Details" button with the technical message.
func (a *App) showError(sentenceKey string, err error) *dialog.CustomDialog {
	details := widget.NewLabel(err.Error())
	details.Wrapping = fyne.TextWrapWord
	details.Hide()
	var more *widget.Button
	more = widget.NewButton(a.tr.T("button.details"), func() { details.Show(); more.Hide() })
	msg := widget.NewLabel(a.tr.T(sentenceKey))
	msg.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustom(a.o.Config.Name.Get(a.tr.Code()), a.tr.T("button.close"), container.NewVBox(msg, more, details), a.win)
	d.Resize(fyne.NewSize(460, 200))
	d.Show()
	return d
}

// setReadmes gives the library the texts of the README.md files in the folders it fills, in the user's language.
func (a *App) setReadmes() {
	name := a.o.Config.Name.Get(a.tr.Code())
	texts := map[string]string{folders.Songs: a.tr.T("readme.songs", "name", name)}
	for kind, ct := range a.o.Config.Content {
		texts[folders.Content(kind)] = a.tr.T("readme.content", "name", name, "label", ct.Label.Get(a.tr.Code()))
	}
	a.o.Library.SetReadmes(texts)
}

// ShowFatal shows an error window for problems before the launcher can work (bad config, second instance) and
// returns when it is closed. sentence is already translated.
func ShowFatal(fa fyne.App, tr *Translator, sentence string, err error) {
	w := fa.NewWindow("Karaoke launcher")
	msg := widget.NewLabel(sentence)
	msg.Wrapping = fyne.TextWrapWord
	details := widget.NewLabel(err.Error())
	details.Wrapping = fyne.TextWrapWord
	details.Hide()
	var more *widget.Button
	more = widget.NewButton(tr.T("button.details"), func() { details.Show(); more.Hide() })
	w.SetContent(container.NewPadded(container.NewVBox(msg, more, details,
		widget.NewButton(tr.T("button.close"), fa.Quit))))
	w.Resize(fyne.NewSize(480, 220))
	w.ShowAndRun()
}
