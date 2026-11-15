package start

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary act as the started program: it writes its arguments and working folder to a file
// and exits with the requested code.
func TestMain(m *testing.M) {
	if out := os.Getenv("START_HELPER_OUT"); out != "" {
		wd, _ := os.Getwd()
		os.WriteFile(out, []byte(wd+"\n"+strings.Join(os.Args[1:], "\n")), 0o644)
		code, _ := strconv.Atoi(os.Getenv("START_HELPER_EXIT"))
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// game copies the test binary into a new app folder as "bin/game".
func game(t *testing.T) (appdir, rel string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	appdir = t.TempDir()
	rel = "bin/game"
	if runtime.GOOS == "windows" {
		rel += ".exe"
	}
	os.MkdirAll(filepath.Join(appdir, "bin"), 0o755)
	if err := os.WriteFile(filepath.Join(appdir, filepath.FromSlash(rel)), data, 0o755); err != nil {
		t.Fatal(err)
	}
	return appdir, rel
}

func TestStart(t *testing.T) {
	appdir, rel := game(t)
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("START_HELPER_OUT", out)
	args := []string{rel, "-songpath", "/lib/songs", "a; rm -rf ~ && $(echo x) | y", "two words"}
	p, err := Start(args, appdir)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(20 * time.Second):
		t.Fatal("program did not end")
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	lines := strings.Split(string(got), "\n")
	wantDir, _ := filepath.EvalSymlinks(filepath.Join(appdir, "bin"))
	gotDir, _ := filepath.EvalSymlinks(lines[0])
	if gotDir != wantDir {
		t.Errorf("working folder %s, want %s", gotDir, wantDir)
	}
	if !reflect.DeepEqual(lines[1:], args[1:]) {
		t.Errorf("arguments %q, want %q", lines[1:], args[1:]) // no shell: every argument arrives as it is
	}
}

func TestExitCode(t *testing.T) {
	appdir, rel := game(t)
	t.Setenv("START_HELPER_OUT", filepath.Join(t.TempDir(), "out"))
	t.Setenv("START_HELPER_EXIT", "3")
	p, err := Start([]string{rel}, appdir)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err == nil || !strings.Contains(err.Error(), "3") {
		t.Fatalf("exit code 3: %v", err)
	}
}

func TestMissing(t *testing.T) {
	if _, err := Start([]string{"no-such-game-program-xyz"}, t.TempDir()); err == nil {
		t.Fatal("missing program started")
	}
	if _, err := Start(nil, t.TempDir()); err == nil {
		t.Fatal("empty command started")
	}
	if got := Resolve([]string{"bin/game", "x"}, t.TempDir()); got[0] != "bin/game" {
		t.Fatalf("not existing in appdir: %q", got)
	}
}
