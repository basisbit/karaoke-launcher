package update

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestRunning(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		if _, ok := Running(t.TempDir()); ok {
			t.Fatal("ok on a system without a process list")
		}
		t.Skip("no process list on " + runtime.GOOS)
	}
	game := t.TempDir()
	if names, ok := Running(game); !ok || len(names) != 0 {
		t.Fatalf("empty folder: %v %v", names, ok)
	}
	// a copy of the test binary runs from the game folder: it waits in TestHelperWait
	self, _ := os.Executable()
	exe := filepath.Join(game, "bin", "game-test"+filepath.Ext(self))
	os.MkdirAll(filepath.Dir(exe), 0o755)
	copyFile(t, self, exe)
	cmd := exec.Command(exe, "-test.run=^TestHelperWait$")
	cmd.Env = append(os.Environ(), "UPDATE_HELPER_WAIT=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	deadline := time.Now().Add(10 * time.Second)
	for {
		names, _ := Running(game)
		if slices.Contains(names, filepath.Base(exe)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("running game not found: %v", names)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if names, _ := Running(t.TempDir()); len(names) != 0 {
		t.Fatalf("other folder: %v", names)
	}
	cmd.Process.Kill()
	cmd.Wait()
	if names, _ := Running(game); len(names) != 0 {
		t.Fatalf("after it ended: %v", names)
	}
}

// TestHelperWait is the "game" of TestRunning.
func TestHelperWait(t *testing.T) {
	if os.Getenv("UPDATE_HELPER_WAIT") == "" {
		t.Skip("helper")
	}
	time.Sleep(time.Minute)
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	out.Close()
}
