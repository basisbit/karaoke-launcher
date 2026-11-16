package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// resizeWatcher is an invisible widget behind the screen content. Fyne lays it out with the window's size, so it
// learns when the window is resized: Fyne's dialogs keep the size they were opened with.
type resizeWatcher struct {
	widget.BaseWidget
	onResize func(fyne.Size)
}

func newResizeWatcher(onResize func(fyne.Size)) *resizeWatcher {
	w := &resizeWatcher{onResize: onResize}
	w.ExtendBaseWidget(w)
	return w
}

func (w *resizeWatcher) Resize(s fyne.Size) {
	w.BaseWidget.Resize(s)
	w.onResize(s)
}

func (w *resizeWatcher) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// setContent shows a screen in the window.
func (a *App) setContent(o fyne.CanvasObject) {
	a.settingsOn = false // showSettings sets it again
	if a.watcher == nil {
		a.watcher = newResizeWatcher(func(s fyne.Size) {
			if a.fullDialog != nil {
				a.fullDialog.Resize(s)
			}
		})
	}
	a.win.SetContent(container.NewStack(a.watcher, o))
}
