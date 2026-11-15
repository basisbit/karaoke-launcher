package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	kfeed "github.com/basisbit/karaoke-feeds/tool"

	"github.com/basisbit/karaoke-launcher/internal/library"
)

// songs is the screen where people choose song packs and start or pause the download.
type songs struct {
	a        *App
	root     fyne.CanvasObject
	list     *fyne.Container
	needs    *widget.Label
	status   *widget.Label
	details  *widget.Button // shown when the status line reports a problem
	progress *widget.ProgressBar
	download *widget.Button
	pause    *widget.Button
	feeds    []*feedView
	planSeq  int // only the newest Plan result is shown
	bottom   *fyne.Container
	noSpace  bool // the chosen packs don't fit
	running  bool // a download was running at the last refresh
}

// feedView is one feed on the songs screen.
type feedView struct {
	feed   library.Feed
	info   *kfeed.Info
	err    error
	checks map[string]*widget.Check
	chosen map[string]bool // packs that are in the library now
}

func (a *App) showSongs() {
	a.home = nil
	s := &songs{a: a, list: container.NewVBox(), needs: widget.NewLabel(""), status: widget.NewLabel(""),
		progress: widget.NewProgressBar()}
	tr := a.tr
	s.needs.Wrapping = fyne.TextWrapWord
	s.status.Wrapping = fyne.TextWrapWord
	s.progress.Hide()
	s.download = widget.NewButtonWithIcon(tr.T("songs.download"), theme.DownloadIcon(), s.start)
	s.download.Importance = widget.HighImportance
	s.pause = widget.NewButtonWithIcon(tr.T("songs.pause"), theme.MediaPauseIcon(), func() { go a.o.Library.Pause() })
	title := widget.NewLabelWithStyle(tr.T("songs.title"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameHeadingText
	back := widget.NewButtonWithIcon(tr.T("button.back"), theme.NavigateBackIcon(), a.showHome)
	top := container.NewBorder(nil, nil, nil, back, title)
	s.details = widget.NewButton(tr.T("button.details"), a.showStatusDetails)
	s.details.Importance = widget.LowImportance
	s.details.Hide()
	s.bottom = container.NewVBox(s.progress, container.NewBorder(nil, nil, nil, s.details, s.status), s.needs,
		container.NewHBox(layout.NewSpacer(), s.pause, s.download))
	s.root = container.NewPadded(container.NewBorder(top, s.bottom, nil, nil, container.NewVScroll(s.list)))
	s.list.Add(widget.NewLabel(tr.T("status.checking")))
	a.songs = s
	a.setContent(s.root)
	s.refresh(a.o.Library.Status())
	go s.load()
}

// load gets the description of every feed (or the one remembered from earlier while a download runs).
func (s *songs) load() {
	a := s.a
	var views []*feedView
	for _, f := range a.o.Library.Feeds() {
		info, err := a.inspect(f.ID)
		views = append(views, &feedView{feed: f, info: info, err: err})
	}
	a.do(func() {
		if a.songs != s {
			return // the screen was left
		}
		s.feeds = views
		s.build()
		s.replan()
	})
}

// inspect returns a feed's description; while a download runs it returns the last one seen. Safe from any goroutine.
func (a *App) inspect(id string) (*kfeed.Info, error) {
	info, err := a.o.Library.Inspect(context.Background(), id)
	a.infoMu.Lock()
	defer a.infoMu.Unlock()
	if err == nil {
		a.infos[id] = info
		return info, nil
	}
	if old := a.infos[id]; old != nil && errors.Is(err, library.ErrBusy) {
		return old, nil
	}
	return nil, err
}

func (s *songs) build() {
	a, tr := s.a, s.a.tr
	s.list.RemoveAll()
	for _, fv := range s.feeds {
		fv.checks, fv.chosen = map[string]*widget.Check{}, map[string]bool{}
		if fv.err != nil {
			key := "error.generic"
			if isOffline(fv.err) {
				key = "error.offline"
			}
			err := fv.err
			s.list.Add(container.NewHBox(widget.NewLabel(tr.T(key)),
				widget.NewButton(tr.T("button.details"), func() { a.showError(key, err) })))
			continue
		}
		info := fv.info
		name := widget.NewLabelWithStyle(info.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		trust := tr.T("songs.unsigned")
		if info.Signed {
			trust = tr.T("songs.signed")
		}
		meta := widget.NewLabel(tr.T("songs.by", "operator", info.OperatorName) + "\n" + trust)
		meta.Wrapping = fyne.TextWrapWord
		s.list.Add(name)
		s.list.Add(meta)
		never := !info.Subscribed // starter packs only for a feed that was never chosen, not after "remove all"
		for _, p := range info.Packs {
			fv.chosen[p.ID] = p.Chosen
			c := widget.NewCheck(pick(p.Names, tr.Code()), nil)
			if c.Text == "" {
				c.Text = p.ID
			}
			c.SetChecked(p.Chosen || (never && slices.Contains(fv.feed.Starter, p.ID)))
			id, view := p.ID, fv
			c.OnChanged = func(on bool) { s.toggled(view, id, on) }
			fv.checks[p.ID] = c
			size := widget.NewLabel(tr.T("songs.pack", "songs", strconv.Itoa(p.Songs), "size", humanBytes(p.Bytes)))
			s.list.Add(container.NewBorder(nil, nil, nil, size, c))
		}
		s.list.Add(widget.NewSeparator())
	}
}

// toggled asks before songs that are in the library are removed; the removal starts right away.
func (s *songs) toggled(fv *feedView, id string, on bool) {
	tr := s.a.tr
	if on || !fv.chosen[id] {
		s.replan()
		return
	}
	c := fv.checks[id]
	d := dialog.NewConfirm(tr.T("songs.remove.title"), tr.T("songs.remove.text", "pack", c.Text), func(yes bool) {
		if !yes {
			c.OnChanged = nil
			c.SetChecked(true)
			c.OnChanged = func(on bool) { s.toggled(fv, id, on) }
			return
		}
		s.start()
	}, s.a.win)
	d.SetConfirmText(tr.T("songs.remove.yes"))
	d.SetDismissText(tr.T("button.cancel"))
	d.Show()
}

// choices returns what to download per feed. A feed that was never chosen and has nothing ticked is left out.
func (s *songs) choices() map[string]library.Choice {
	out := map[string]library.Choice{}
	for _, fv := range s.feeds {
		if fv.info == nil {
			continue
		}
		packs := []string{}
		for _, p := range fv.info.Packs {
			if fv.checks[p.ID].Checked {
				packs = append(packs, p.ID)
			}
		}
		if len(packs) > 0 || fv.info.Subscribed {
			out[fv.feed.ID] = library.Choice{Packs: packs, Exclude: s.a.exclude()}
		}
	}
	return out
}

// replan updates the "Needs …, … free" line in the background.
func (s *songs) replan() {
	a := s.a
	s.planSeq++
	seq := s.planSeq
	choices := s.choices()
	go func() {
		var need int64
		var err error
		for id, c := range choices {
			n, _, e := a.o.Library.Plan(context.Background(), id, c)
			need += n
			if e != nil && err == nil {
				err = e
			}
		}
		free, ferr := freeSpace(a.o.Library.Root())
		a.do(func() {
			if a.songs != s || seq != s.planSeq {
				return
			}
			s.showPlan(need, free, errors.Join(err, ferr))
		})
	}()
}

func (s *songs) showPlan(need, free int64, err error) {
	tr := s.a.tr
	if err != nil {
		s.needs.SetText("")
		return
	}
	s.noSpace = need > free
	if s.noSpace {
		s.needs.SetText(tr.T("songs.nospace", "size", humanBytes(need), "free", humanBytes(free)))
		s.download.Disable()
		return
	}
	s.needs.SetText(tr.T("songs.needs", "size", humanBytes(need), "free", humanBytes(free)))
	s.refresh(s.a.o.Library.Status())
}

// freeSpace measures the drive of dir, or of its nearest existing parent folder.
func freeSpace(dir string) (int64, error) {
	d, err := existingDir(dir)
	if err != nil {
		return 0, err
	}
	return kfeed.FreeSpace(d)
}

// existingDir returns dir, or its nearest parent folder that exists.
func existingDir(dir string) (string, error) {
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no existing folder")
		}
		dir = parent
	}
}

func (s *songs) start() {
	a := s.a
	if err := a.o.Library.Start(s.choices()); err != nil && !errors.Is(err, library.ErrBusy) {
		a.showError("error.generic", err)
	}
}

// refresh shows the download state on this screen.
func (s *songs) refresh(st library.Status) {
	tr := s.a.tr
	if st.Running || st.Paused || st.Err != nil || len(st.Results) > 0 {
		s.status.SetText(statusText(tr, st, s.a.gameRunning))
	}
	setVisible(s.details, !st.Running && !st.Paused && !s.a.gameRunning && statusDetails(st) != "")
	showBar := (st.Running || st.Paused) && st.Progress.BytesTotal > 0 // a paused download keeps its bar
	if showBar {
		s.progress.SetValue(float64(st.Progress.BytesDone) / float64(st.Progress.BytesTotal))
	}
	if setVisible(s.progress, showBar) {
		s.root.Refresh()
	}
	anyChosen := false
	for _, fv := range s.feeds {
		for _, on := range fv.chosen {
			anyChosen = anyChosen || on
		}
	}
	switch {
	case !st.Running && st.Paused:
		s.download.SetText(tr.T("songs.resume"))
		s.download.SetIcon(theme.MediaPlayIcon())
	case anyChosen:
		s.download.SetText(tr.T("songs.update"))
		s.download.SetIcon(theme.DownloadIcon())
	default:
		s.download.SetText(tr.T("songs.download"))
		s.download.SetIcon(theme.DownloadIcon())
	}
	if st.Running || s.noSpace {
		s.download.Disable()
	} else {
		s.download.Enable()
	}
	if st.Running {
		s.pause.Enable()
	} else {
		s.pause.Disable()
	}
	if s.running && !st.Running {
		go s.load() // a download ended: the packs in the library changed
	}
	s.running = st.Running
}
