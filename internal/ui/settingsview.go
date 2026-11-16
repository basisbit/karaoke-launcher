package ui

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/basisbit/karaoke-launcher/internal/config"
	"github.com/basisbit/karaoke-launcher/internal/folders"
	"github.com/basisbit/karaoke-launcher/internal/library"
	"github.com/basisbit/karaoke-launcher/internal/settings"
)

// exclude returns the licence filter as the engine wants it: never nil, so that it is always applied.
func (a *App) exclude() []string {
	return append([]string{}, a.o.Settings.Exclude...)
}

// save writes the settings; a problem is shown.
func (a *App) save() bool {
	if err := a.o.Settings.Save(); err != nil {
		a.showError("error.generic", err)
		return false
	}
	return true
}

// reshowSettings builds the settings screen again after a dialog, but only while it is on show (the user may have
// gone back to Home meanwhile).
func (a *App) reshowSettings() {
	if a.settingsOn {
		a.showSettings()
	}
}

func (a *App) showSettings() {
	a.home, a.songs = nil, nil
	defer func() { a.settingsOn = true }() // after setContent, which clears it
	tr, set := a.tr, a.o.Settings

	// song folder
	folder := widget.NewLabel(set.Library)
	folder.Wrapping = fyne.TextWrapBreak
	note := widget.NewLabel("")
	note.Wrapping = fyne.TextWrapWord
	note.Hide()
	change := widget.NewButton(tr.T("settings.library.change"), func() {
		d := dialog.NewFolderOpen(func(u fyne.ListableURI, err error) {
			a.fullDialog = nil
			if err != nil || u == nil {
				return
			}
			if !a.songFolderOK(u.Path()) {
				dialog.ShowInformation(tr.T("settings.library.change"), tr.T("settings.library.bad"), a.win)
				return
			}
			if err := a.o.Library.SetRoot(u.Path()); err != nil {
				a.showError("error.generic", err)
				return
			}
			set.Library = u.Path()
			a.o.Library.SetTargets(a.o.Config.Targets(a.o.GOOS, a.vars())) // folders given as {library}/… move along
			a.setReadmes()
			if a.save() {
				folder.SetText(set.Library)
				note.SetText(tr.T("settings.library.new"))
				note.Show()
			}
		}, a.win)
		if dir, err := existingDir(set.Library); err == nil { // start in the current folder, not its parent
			if l, err := storage.ListerForURI(storage.NewFileURI(dir)); err == nil {
				d.SetLocation(l)
			}
		}
		// it is drawn inside the window and can't be resized by its edges: it follows the window size instead
		d.Resize(a.win.Canvas().Size())
		a.fullDialog = d
		d.Show()
	})

	// language
	codes := Languages()
	names := []string{tr.T("settings.language.system")}
	for _, c := range codes {
		names = append(names, LanguageName(c))
	}
	language := widget.NewSelect(names, nil)
	language.SetSelectedIndex(0)
	if i := slices.Index(codes, set.Language); i >= 0 {
		language.SetSelectedIndex(i + 1)
	}
	language.OnChanged = func(string) {
		code := ""
		if i := language.SelectedIndex(); i > 0 {
			code = codes[i-1]
		}
		set.Language = code
		if a.save() {
			a.tr = NewTranslator(code)
			a.win.SetTitle(a.o.Config.Name.Get(a.tr.Code()))
			a.setReadmes()
			a.showSettings() // all texts in the new language
		}
	}

	// licence filter
	licences := container.NewVBox()
	for _, p := range []string{"nc", "nd", "sa"} {
		c := widget.NewCheck("", nil) // the text is a separate label, because a checkbox text does not wrap
		c.SetChecked(slices.Contains(set.Exclude, p))
		prop := p
		c.OnChanged = func(on bool) {
			ex := slices.DeleteFunc(slices.Clone(set.Exclude), func(s string) bool { return s == prop })
			if on {
				ex = append(ex, prop)
			}
			set.Exclude = ex
			if a.save() && a.o.Library.HasSongs() {
				a.applyExclude()
			}
		}
		text := widget.NewLabel(tr.T("settings.license." + p))
		text.Wrapping = fyne.TextWrapWord
		licences.Add(container.NewBorder(nil, nil, c, nil, text))
	}

	items := []fyne.CanvasObject{
		section(tr.T("settings.library")), folder, container.NewHBox(change), note,
		section(tr.T("settings.language")), language,
		section(tr.T("settings.licenses")), licences,
	}
	items = append(items, section(tr.T("settings.import")), a.importBox())
	if a.o.Config.AllowOtherFeeds {
		items = append(items, section(tr.T("settings.sources")), a.sourcesBox())
	}
	items = append(items, widget.NewSeparator(), container.NewHBox(
		widget.NewButton(tr.T("settings.open.songs"), func() { a.openFolder(a.o.Library.Root()) }),
		widget.NewButton(tr.T("settings.open.settings"), func() { a.openFolder(a.o.UserData) }),
		widget.NewButton(tr.T("about.title"), a.showAbout),
	))

	title := widget.NewLabelWithStyle(tr.T("settings.title"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameHeadingText
	back := widget.NewButtonWithIcon(tr.T("button.back"), theme.NavigateBackIcon(), a.showHome)
	a.setContent(container.NewPadded(container.NewBorder(container.NewBorder(nil, nil, nil, back, title), nil, nil, nil,
		container.NewVScroll(container.NewVBox(items...)))))
}

func section(text string) fyne.CanvasObject {
	return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

// applyExclude starts an update so that the licence filter takes effect on the songs that are there. A running
// download is paused for it and continues with the filter.
func (a *App) applyExclude() {
	choices := map[string]library.Choice{}
	for _, f := range a.o.Library.Feeds() {
		choices[f.ID] = library.Choice{Exclude: a.exclude()}
	}
	lib := a.o.Library
	go func() {
		if lib.Status().Running {
			lib.Pause() // waits until it stopped
		}
		if err := lib.Start(choices); err != nil && !errors.Is(err, library.ErrBusy) {
			a.do(func() { a.showError("error.generic", err) })
		}
	}()
}

// sourcesBox lists the feeds the user added, with Remove, and a field to add one.
func (a *App) sourcesBox() fyne.CanvasObject {
	tr, set := a.tr, a.o.Settings
	box := container.NewVBox()
	for _, f := range set.Feeds {
		feed := f
		label := widget.NewLabel(feed.Address)
		label.Wrapping = fyne.TextWrapBreak
		remove := widget.NewButton(tr.T("settings.sources.remove"), func() { a.removeSource(feed) })
		box.Add(container.NewBorder(nil, nil, nil, remove, label))
	}
	address := widget.NewEntry()
	address.SetPlaceHolder("https://…")
	add := widget.NewButton(tr.T("settings.sources.add"), func() { a.addSource(strings.TrimSpace(address.Text)) })
	box.Add(container.NewBorder(nil, nil, widget.NewLabel(tr.T("settings.sources.address")), add, address))
	return box
}

// importBox lists the folders with songs the user has already, with Remove, and a button to add one.
func (a *App) importBox() fyne.CanvasObject {
	tr, set := a.tr, a.o.Settings
	note := widget.NewLabel(tr.T("settings.import.note"))
	note.Wrapping = fyne.TextWrapWord
	box := container.NewVBox(note)
	changed := func() {
		if a.save() {
			a.o.Library.SetSeeds(set.Import)
			a.reshowSettings()
		}
	}
	for _, dir := range set.Import {
		d := dir
		label := widget.NewLabel(d)
		label.Wrapping = fyne.TextWrapBreak
		remove := widget.NewButton(tr.T("settings.sources.remove"), func() {
			set.Import = slices.DeleteFunc(slices.Clone(set.Import), func(x string) bool { return x == d })
			changed()
		})
		box.Add(container.NewBorder(nil, nil, nil, remove, label))
	}
	add := widget.NewButton(tr.T("settings.import.add"), func() {
		dlg := dialog.NewFolderOpen(func(u fyne.ListableURI, err error) {
			a.fullDialog = nil
			if err != nil || u == nil || slices.Contains(set.Import, u.Path()) {
				return
			}
			set.Import = append(set.Import, u.Path())
			changed()
		}, a.win)
		dlg.Resize(a.win.Canvas().Size())
		a.fullDialog = dlg
		dlg.Show()
	})
	box.Add(container.NewHBox(add))
	return box
}

// LibraryFeeds is the feed list of the library: the feeds of launcher.json (trusted with packs of other kinds when
// they have a key) and, when launcher.json allows it, the ones the user added (never trusted).
func LibraryFeeds(cfg *config.Config, set *settings.Settings) []library.Feed {
	var feeds []library.Feed
	for _, f := range cfg.Feeds {
		feeds = append(feeds, library.Feed{ID: f.ID, Address: f.Address, Key: f.Key, Starter: f.Starter, Trusted: f.Key != ""})
	}
	if cfg.AllowOtherFeeds {
		for _, f := range set.Feeds {
			feeds = append(feeds, library.Feed{ID: library.AddedID(f.Address), Address: f.Address, Key: f.Key})
		}
	}
	return feeds
}

func (a *App) libraryFeeds() []library.Feed { return LibraryFeeds(a.o.Config, a.o.Settings) }

// addSource checks a feed, shows who runs it and whether it is signed, and adds it after a confirmation.
func (a *App) addSource(address string) {
	tr, set := a.tr, a.o.Settings
	if err := config.CheckURL(strings.Split(address, "#")[0]); err != nil {
		a.showError("error.generic", err)
		return
	}
	id := library.AddedID(address)
	for _, f := range a.libraryFeeds() {
		if f.ID == id {
			return // already there
		}
	}
	feeds := append(a.libraryFeeds(), library.Feed{ID: id, Address: address})
	if err := a.o.Library.SetFeeds(feeds); err != nil {
		a.showError("error.generic", err)
		return
	}
	dir := a.o.Library.Dir(id)
	_, statErr := os.Stat(dir)
	isNew := errors.Is(statErr, os.ErrNotExist)
	undo := func() {
		a.o.Library.SetFeeds(a.libraryFeeds())
		if isNew {
			os.RemoveAll(dir) // only the engine's state folder can be in it
		}
	}
	go func() {
		info, err := a.o.Library.Inspect(context.Background(), id)
		a.do(func() {
			if err != nil {
				undo()
				key := "error.generic"
				if isOffline(err) {
					key = "error.offline"
				}
				a.showError(key, err)
				return
			}
			trust := tr.T("songs.unsigned")
			if info.Signed {
				trust = tr.T("songs.signed")
			}
			text := widget.NewLabel(info.Name + "\n" + tr.T("songs.by", "operator", info.OperatorName) + "\n" + trust)
			text.Wrapping = fyne.TextWrapWord
			d := dialog.NewCustomConfirm(tr.T("settings.sources.confirm"), tr.T("settings.sources.add"), tr.T("button.cancel"),
				text, func(yes bool) {
					if !yes {
						undo()
						return
					}
					set.Feeds = append(set.Feeds, settings.Feed{Address: address})
					if a.save() {
						a.reshowSettings()
					}
				}, a.win)
			d.Resize(fyne.NewSize(460, 240))
			d.Show()
		})
	}()
}

// removeSource removes a feed the user added, and its songs, after a confirmation.
func (a *App) removeSource(f settings.Feed) {
	tr, set := a.tr, a.o.Settings
	d := dialog.NewConfirm(tr.T("songs.remove.title"), tr.T("songs.remove.text", "pack", f.Address), func(yes bool) {
		if !yes {
			return
		}
		set.Feeds = slices.DeleteFunc(slices.Clone(set.Feeds), func(x settings.Feed) bool { return x.Address == f.Address })
		if err := a.o.Library.SetFeeds(a.libraryFeeds()); err != nil {
			a.showError("error.generic", err)
			return
		}
		if !a.save() {
			return
		}
		if err := os.RemoveAll(a.o.Library.Dir(library.AddedID(f.Address))); err != nil {
			a.showError("error.generic", err)
		}
		a.reshowSettings()
	}, a.win)
	d.SetConfirmText(tr.T("songs.remove.yes"))
	d.SetDismissText(tr.T("button.cancel"))
	d.Show()
}

// openFolder shows a folder in the system's file manager.
func (a *App) openFolder(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.showError("error.generic", err)
		return
	}
	p := filepath.ToSlash(dir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/… on Windows
	}
	if err := a.fa.OpenURL(&url.URL{Scheme: "file", Path: p}); err != nil {
		a.showError("error.generic", err)
	}
}

// showAbout shows the versions and the full licence texts of everything the program is built from, as a screen
// (it follows the window size; the texts are plain, fixed-width and not selectable).
func (a *App) showAbout() {
	tr := a.tr
	engine := "?"
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, m := range bi.Deps {
			if m.Path == "github.com/basisbit/karaoke-feeds/tool" {
				engine = m.Version
				if m.Replace != nil {
					engine = "local"
				}
			}
		}
	}
	head := widget.NewLabel(tr.T("about.launcher", "version", a.o.Version) + "\n" +
		tr.T("about.engine", "version", engine) + "\n\n" + tr.T("about.libraries"))
	// one part at a time (Fyne draws thousands of lines slowly), as wrapping paragraphs with a heading per file
	sections := licenceSections()
	names := make([]string, len(sections))
	for i, x := range sections {
		names[i] = x.title
	}
	texts := container.NewVBox()
	textScroll := container.NewVScroll(texts)
	pickPart := widget.NewSelect(names, func(name string) {
		i := slices.Index(names, name)
		if i < 0 {
			return
		}
		texts.RemoveAll()
		for _, f := range sections[i].files() {
			texts.Add(widget.NewLabelWithStyle(f.name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
			l := widget.NewLabel(reflow(f.text))
			l.Wrapping = fyne.TextWrapWord
			texts.Add(l)
		}
		textScroll.ScrollToTop()
	})
	if len(names) > 0 {
		pickPart.SetSelectedIndex(0)
	}
	licences := container.NewBorder(pickPart, nil, nil, nil, textScroll)
	title := widget.NewLabelWithStyle(tr.T("about.title"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameHeadingText
	back := widget.NewButtonWithIcon(tr.T("button.back"), theme.NavigateBackIcon(), a.showSettings)
	a.setContent(container.NewPadded(container.NewBorder(
		container.NewVBox(container.NewBorder(nil, nil, nil, back, title), head), nil, nil, nil, licences)))
}

// songFolderOK reports whether dir can be the song folder. It can't be a folder the launcher fills with something
// else (marked by its README.md), be inside such a folder or inside another song folder, be or hold the folder of
// the launcher or of a program it installs, or be, hold or be inside a folder for themes and other content (except
// such folders inside the song folder, which move along with it).
func (a *App) songFolderOK(dir string) bool {
	if p := folders.Purpose(dir); p != "" && p != folders.Songs {
		return false
	}
	if folders.MarkedParent(dir) != "" {
		return false
	}
	if m, _ := filepath.Glob(filepath.Join(dir, ".component-*.json")); len(m) > 0 {
		return false // a program folder
	}
	v := a.vars()
	v.Library = dir
	busy := []string{a.o.AppDir}
	for id := range a.o.Config.Components {
		if d, ok, err := a.o.Config.ComponentDir(id, a.o.GOOS, v); ok && err == nil {
			busy = append(busy, d)
		}
	}
	for _, d := range busy {
		if folders.Within(d, dir) {
			return false
		}
	}
	for kind := range a.o.Config.Content {
		d, ok, err := a.o.Config.ContentDir(kind, a.o.GOOS, v)
		if ok && err == nil && !a.o.Config.ContentInLibrary(kind, a.o.GOOS) && folders.Overlap(d, dir) {
			return false
		}
	}
	return true
}
