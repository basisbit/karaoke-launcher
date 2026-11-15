package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRelaunchHelper is the "launcher" that TestRelaunch starts: a copy of this test binary. It installs a staged
// update of itself and starts again (as main.go does), or, when nothing is staged, writes its version into a file.
func TestRelaunchHelper(t *testing.T) {
	out := os.Getenv("RELAUNCH_OUT")
	if out == "" {
		t.Skip("helper for TestRelaunch")
	}
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	Cleanup(dir)
	if Pending(dir, "launcher") {
		if _, err := Apply(dir, "launcher", nil); err != nil {
			os.WriteFile(out, []byte("apply: "+err.Error()), 0o644)
			return
		}
		if err := Relaunch(); err != nil {
			os.WriteFile(out, []byte("relaunch: "+err.Error()), 0o644)
		}
		return
	}
	st, err := Installed(dir, "launcher")
	if err != nil || st == nil {
		os.WriteFile(out, []byte("no state"), 0o644)
		return
	}
	os.WriteFile(out, []byte("version "+st.Version), 0o644)
}

func TestRelaunch(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	name := "launcher"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Join(t.TempDir(), "app")
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o755); err != nil {
		t.Fatal(err)
	}
	WriteState(dir, State{Component: "launcher", Version: "1.0", Files: []string{name}})
	stage(t, dir, "launcher", "2.0", map[string]string{name: string(data)})
	os.Chmod(filepath.Join(stagingDir(dir, "launcher"), "files", name), 0o755)

	out := filepath.Join(t.TempDir(), "out.txt")
	cmd := exec.Command(filepath.Join(dir, name), "-test.run=^TestRelaunchHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "RELAUNCH_OUT="+out)
	if err := cmd.Run(); err != nil {
		t.Fatal("first start:", err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		got, _ := os.ReadFile(out)
		if s := string(got); s != "" {
			if s != "version 2.0" {
				t.Fatalf("restarted launcher says %q, want version 2.0", s)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the launcher did not start again")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if Pending(dir, "launcher") {
		t.Fatal("update still staged")
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Logf("folder after the restart: %s", strings.Join(names, " "))
}
