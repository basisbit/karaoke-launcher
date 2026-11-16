package ui

import (
	"path/filepath"
	"testing"
	"time"
)

// TestRaiseOnSecondStart: a second start of the launcher (AskToRaise) brings the open window to the front.
func TestRaiseOnSecondStart(t *testing.T) {
	a := testApp(t, filepath.Join("..", "..", "examples", "light", "launcher.json"), "en")
	raised := make(chan struct{}, 4)
	a.raise = func() { raised <- struct{}{} }
	stop := a.watchRaise()
	defer stop()
	for i := 0; i < 2; i++ { // also a third start
		if err := AskToRaise(a.o.UserData); err != nil {
			t.Fatal(err)
		}
		select {
		case <-raised:
		case <-time.After(5 * time.Second):
			t.Fatalf("start %d: the window was not raised", i+2)
		}
	}
}
