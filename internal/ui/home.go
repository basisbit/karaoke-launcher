package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/basisbit/karaoke-launcher/internal/library"
	"github.com/basisbit/karaoke-launcher/internal/start"
)

// home is the first screen: logo, name, news, Play, status, links.
type home struct {
	a        *App
	root     fyne.CanvasObject
	play     *widget.Button
	hint     *widget.Label // replaces Play when the game can't be started on this system
	status   *widget.Label
	details  *widget.Button // shown when the status line reports a problem
	progress *widget.ProgressBar
	news     *widget.Label
	newsBox  *fyne.Container
	bottom   *fyne.Container
}

func newHome(a *App) *home {
	h := &home{a: a, status: widget.NewLabel(""), progress: widget.NewProgressBar(), news: widget.NewLabel("")}
	tr, c := a.tr, a.o.Config

	// top right: navigation
	nav := container.NewHBox(layout.NewSpacer(),
		widget.NewButtonWithIcon(tr.T("home.songs"), theme.ListIcon(), a.showSongs),
		widget.NewButtonWithIcon(tr.T("home.settings"), theme.SettingsIcon(), a.showSettings),
	)

	// the centre column: logo, name, news, then status and Play
	var logo fyne.CanvasObject = layout.NewSpacer()
	if p := c.Asset(c.Look.Logo); p != "" {
		img := canvas.NewImageFromFile(p)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(140, 140))
		logo = container.NewCenter(img)
	}
	title := widget.NewLabelWithStyle(c.Name.Get(tr.Code()), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameHeadingText
	title.Wrapping = fyne.TextWrapWord

	h.news.Wrapping = fyne.TextWrapWord
	newsContent := container.NewVBox(h.news)
	if n := c.News; n != nil && n.More != "" {
		more := widget.NewButton(tr.T("home.news.more"), func() { a.openURL(n.More) })
		more.Importance = widget.LowImportance
		newsContent.Add(container.NewHBox(more))
	}
	h.newsBox = container.NewVBox(widget.NewCard("", "", newsContent))
	h.newsBox.Hide()
	if a.newsText != "" {
		h.setNews(a.newsText)
	}

	h.play = widget.NewButtonWithIcon(tr.T("home.play"), theme.MediaPlayIcon(), h.onPlay)
	h.play.Importance = widget.HighImportance
	h.hint = widget.NewLabelWithStyle(tr.T("home.play.unsupported"), fyne.TextAlignCenter, fyne.TextStyle{})
	h.hint.Wrapping = fyne.TextWrapWord
	if _, ok, _ := c.StartArgs(a.o.GOOS, a.vars()); ok {
		h.hint.Hide()
	} else {
		h.play.Hide()
	}
	h.status.Wrapping = fyne.TextWrapWord
	h.status.Alignment = fyne.TextAlignCenter
	h.details = widget.NewButton(tr.T("button.details"), func() {
		a.showStatusDetails()
	})
	h.details.Importance = widget.LowImportance
	h.details.Hide()
	h.progress.Hide()
	h.bottom = container.NewVBox(h.progress, h.status, container.NewCenter(h.details),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(320, 72), h.play)), h.hint)
	column := container.NewVBox(layout.NewSpacer(), logo, title, h.newsBox, layout.NewSpacer(), h.bottom, layout.NewSpacer())

	// bottom: links
	links := container.NewHBox(layout.NewSpacer())
	for _, l := range c.Links {
		u := l.URL
		b := widget.NewButton(pick(l.Label, tr.Code()), func() { a.openURL(u) })
		b.Importance = widget.LowImportance
		links.Add(b)
	}
	links.Add(layout.NewSpacer())

	content := container.NewPadded(container.NewBorder(nav, links, nil, nil,
		container.New(columnLayout{max: 560}, container.NewVScroll(column))))
	h.root = content
	if p := c.Asset(c.Look.Background); p != "" {
		bg := canvas.NewImageFromFile(p)
		bg.FillMode = canvas.ImageFillStretch
		h.root = container.NewStack(bg, content)
	}
	return h
}

func (h *home) setNews(text string) {
	h.news.SetText(text)
	if setVisible(h.newsBox, true) && h.root != nil {
		h.root.Refresh() // lay out again: the news take their place in the column
	}
}

// refresh shows the library status. Play works as soon as one song is there; before that it leads to the songs.
func (h *home) refresh(s library.Status) {
	a := h.a
	if !s.Running && !s.Paused && s.Err == nil && len(s.Results) == 0 && !a.o.Library.HasSongs() {
		h.status.SetText("") // nothing downloaded yet: "up to date" would be misleading
	} else {
		h.status.SetText(statusText(a.tr, s, a.gameRunning))
	}
	setVisible(h.details, !s.Running && !a.gameRunning && statusDetails(s) != "" && !s.Paused)
	showBar := (s.Running || s.Paused) && s.Progress.BytesTotal > 0 // a paused download keeps its bar
	if showBar {
		h.progress.SetValue(float64(s.Progress.BytesDone) / float64(s.Progress.BytesTotal))
	}
	if setVisible(h.progress, showBar) {
		h.root.Refresh() // lay out the whole screen again: the progress bar takes its place
	}
	switch {
	case a.gameRunning:
		h.play.Disable()
	case a.o.Library.HasSongs():
		h.play.SetText(a.tr.T("home.play"))
		h.play.SetIcon(theme.MediaPlayIcon())
		h.play.Enable()
	default:
		h.play.SetText(a.tr.T("home.play.choose"))
		h.play.SetIcon(theme.ListIcon())
		h.play.Enable()
	}
}

// onPlay starts the game, or leads to the songs when there are none yet. A running download pauses while the
// game runs and continues when it ends.
func (h *home) onPlay() {
	a := h.a
	if !a.o.Library.HasSongs() {
		a.showSongs()
		return
	}
	args, _, err := a.o.Config.StartArgs(a.o.GOOS, a.vars())
	if err != nil {
		a.showError("error.generic", err)
		return
	}
	a.resume = a.o.Library.Status().Running
	if a.resume {
		a.o.Library.Pause()
	}
	p, err := start.Start(args, a.o.AppDir)
	if err != nil {
		a.showError("error.start", err)
		a.resumeDownload()
		return
	}
	a.gameRunning = true
	h.refresh(a.o.Library.Status())
	go func() {
		<-p.Done()
		a.do(func() {
			a.gameRunning = false
			a.resumeDownload()
			a.statusChanged(a.o.Library.Status())
		})
	}()
}

func (a *App) resumeDownload() {
	if a.resume {
		a.resume = false
		if err := a.o.Library.Start(nil); err != nil && err != library.ErrBusy {
			a.showError("error.generic", err)
		}
	}
}

// columnLayout gives its objects the full height and at most max width, centred: a column that doesn't stretch
// across a wide window.
type columnLayout struct{ max float32 }

func (l columnLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	w := min(size.Width, l.max)
	for _, o := range objs {
		o.Resize(fyne.NewSize(w, size.Height))
		o.Move(fyne.NewPos((size.Width-w)/2, 0))
	}
}

func (l columnLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var m fyne.Size
	for _, o := range objs {
		m = m.Max(o.MinSize())
	}
	return m
}
