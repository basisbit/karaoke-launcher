package update

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// stage puts an unpacked update of a component into dir, as Stage leaves it.
func stage(t *testing.T, dir, id, version string, files map[string]string) {
	t.Helper()
	os.MkdirAll(stagingDir(dir, id), 0o755)
	var list []string
	for rel, body := range files {
		p := filepath.Join(stagingDir(dir, id), "files", filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		list = append(list, rel)
	}
	slices.Sort(list)
	from := ""
	if st, _ := Installed(dir, id); st != nil {
		from = st.Version
	}
	if err := writeJSON(readyPath(dir, id), ready{Version: version, From: from, Files: list}); err != nil {
		t.Fatal(err)
	}
}

func apply(t *testing.T, dir, id string, keep []string) {
	t.Helper()
	if ok, err := Apply(dir, id, keep); !ok || err != nil {
		t.Fatalf("Apply: %v %v", ok, err)
	}
	if _, err := os.Stat(stagingDir(dir, id)); err == nil {
		t.Fatal("staging folder left")
	}
}

func state(t *testing.T, dir, id string) State {
	t.Helper()
	s, err := Installed(dir, id)
	if err != nil || s == nil {
		t.Fatalf("Installed: %v %v", s, err)
	}
	slices.Sort(s.Files)
	return *s
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	keep := []string{"config.ini", "*.db"}
	if ok, err := Apply(dir, "game", keep); ok || err != nil {
		t.Fatalf("nothing staged: %v %v", ok, err)
	}
	stage(t, dir, "game", "1.0", map[string]string{"game.exe": "v1", "data/old/x.bin": "x", "data/a.bin": "a1", "config.ini": "default", "data/scores.db": "empty"})
	apply(t, dir, "game", keep)
	s := state(t, dir, "game")
	if s.Version != "1.0" || s.Component != "game" || !slices.Equal(s.Files, []string{"data/a.bin", "data/old/x.bin", "game.exe"}) {
		t.Fatalf("state after first install: %+v", s) // kept files are installed, but not listed
	}
	// the user changes kept files and adds own files
	os.WriteFile(filepath.Join(dir, "config.ini"), []byte("mine"), 0o644)
	os.Remove(filepath.Join(dir, "data", "scores.db"))
	os.WriteFile(filepath.Join(dir, "data", "old", "own.txt"), []byte("own"), 0o644)
	os.WriteFile(filepath.Join(dir, "highscores.txt"), []byte("own"), 0o644)

	stage(t, dir, "game", "2.0", map[string]string{"game.exe": "v2", "data/a.bin": "a2", "data/new/n.bin": "n", "config.ini": "new default", "data/scores.db": "empty"})
	os.MkdirAll(filepath.Join(dir, "data", "gone"), 0o755)
	apply(t, dir, "game", keep)
	want := map[string]string{
		".component-game.json": "", "game.exe": "v2", "data/a.bin": "a2", "data/new/n.bin": "n",
		"config.ini":       "mine",                         // kept: exists
		"data/scores.db":   "empty",                        // kept pattern, but missing: installed
		"data/old/own.txt": "own", "highscores.txt": "own", // never written by the launcher
	}
	got := tree(t, dir)
	for name, body := range want {
		if g, ok := got[name]; !ok || (body != "" && g != body) {
			t.Errorf("%s: %q, want %q", name, g, body)
		}
	}
	if len(got) != len(want) {
		t.Errorf("files: %v", got) // data/old/x.bin (stale) must be gone
	}
	if s := state(t, dir, "game"); s.Version != "2.0" || !slices.Equal(s.Files, []string{"data/a.bin", "data/new/n.bin", "game.exe"}) {
		t.Fatalf("state: %+v", s)
	}
	// a folder emptied by the update is removed; one with own files stays; dir itself stays
	stage(t, dir, "game", "3.0", map[string]string{"game.exe": "v3"})
	apply(t, dir, "game", keep)
	if _, err := os.Stat(filepath.Join(dir, "data", "new")); err == nil {
		t.Error("empty folder data/new left")
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "old", "own.txt")); err != nil {
		t.Error("own file removed")
	}
	stage(t, dir, "game", "4.0", map[string]string{})
	apply(t, dir, "game", nil)
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("component folder removed")
	}
}

func TestApplyInterrupted(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{}
	for i := range 10 {
		files["d/f"+string(rune('a'+i))] = "new " + string(rune('a'+i))
	}
	stage(t, dir, "game", "2.0", files)
	// an interrupted run moved half of the files already
	staged := filepath.Join(stagingDir(dir, "game"), "files")
	os.MkdirAll(filepath.Join(dir, "d"), 0o755)
	for i := range 5 {
		name := "f" + string(rune('a'+i))
		if err := os.Rename(filepath.Join(staged, "d", name), filepath.Join(dir, "d", name)); err != nil {
			t.Fatal(err)
		}
	}
	apply(t, dir, "game", nil)
	got := tree(t, dir)
	for rel, body := range files {
		if got[rel] != body {
			t.Errorf("%s: %q", rel, got[rel])
		}
	}
	if s := state(t, dir, "game"); len(s.Files) != 10 {
		t.Fatalf("state: %+v", s)
	}
}

func TestApplyLockedTarget(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "launcher.exe"), []byte("v1"), 0o755)
	WriteState(dir, State{Component: "launcher", Version: "1", Files: []string{"launcher.exe"}})
	stage(t, dir, "launcher", "2", map[string]string{"launcher.exe": "v2"})
	defer func() { rename = os.Rename }()
	failed := 0
	rename = func(from, to string) error {
		if filepath.Base(to) == "launcher.exe" && filepath.Base(from) == "launcher.exe" && strings.Contains(from, ".update-") && failed == 0 {
			failed++ // the running program can't be overwritten, but it can be renamed
			return errors.New("in use")
		}
		return os.Rename(from, to)
	}
	apply(t, dir, "launcher", nil)
	got := tree(t, dir)
	var old string
	for name := range got {
		if strings.HasPrefix(name, "launcher.exe.old-") {
			old = name
		}
	}
	if got["launcher.exe"] != "v2" || old == "" || got[old] != "v1" || failed != 1 {
		t.Fatalf("after apply: %v", got)
	}
	if list, _ := os.ReadFile(oldListPath(dir)); strings.TrimSpace(string(list)) != old {
		t.Fatalf(".update-old.txt: %q", list)
	}
	if err := Cleanup(dir); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, dir); len(got) != 2 || got["launcher.exe"] != "v2" {
		t.Fatalf("after cleanup: %v", got) // launcher.exe + .component-launcher.json
	}
	if err := Cleanup(dir); err != nil {
		t.Fatal("Cleanup without a list:", err)
	}
	// a hostile list deletes nothing outside, and nothing that is not an .old- file
	outside := filepath.Join(t.TempDir(), "x.old-1")
	os.WriteFile(outside, nil, 0o644)
	os.WriteFile(oldListPath(dir), []byte("../"+filepath.Base(filepath.Dir(outside))+"/x.old-1\n"+outside+"\nlauncher.exe\n"), 0o644)
	Cleanup(dir)
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("Cleanup deleted a file outside the folder")
	}
	if _, err := os.Stat(filepath.Join(dir, "launcher.exe")); err != nil {
		t.Fatal("Cleanup deleted a file that was not moved aside")
	}
}

func TestApplyUnsafeList(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(stagingDir(dir, "game")), 0o755)
	writeJSON(readyPath(dir, "game"), ready{Version: "1", Files: []string{"../evil"}})
	if _, err := Apply(dir, "game", nil); err == nil {
		t.Fatal("unsafe path in ready.json accepted")
	}
	// a stale state that lists unsafe paths deletes nothing outside
	outside := filepath.Join(filepath.Dir(dir), "victim.txt")
	os.WriteFile(outside, []byte("x"), 0o644)
	defer os.Remove(outside)
	WriteState(dir, State{Component: "game", Version: "1", Files: []string{"../victim.txt", outside}})
	stage(t, dir, "game", "2", map[string]string{"a": "a"})
	apply(t, dir, "game", nil)
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("Apply deleted a file outside the folder")
	}
}

func TestApplyCaseOnly(t *testing.T) {
	defer func(f bool) { foldCase = f }(foldCase)
	foldCase = true // as on Windows and macOS
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Game.exe"), []byte("v1"), 0o755)
	WriteState(dir, State{Component: "game", Version: "1", Files: []string{"Game.exe"}})
	stage(t, dir, "game", "2", map[string]string{"game.exe": "v2"})
	apply(t, dir, "game", nil)
	// on a case-insensitive disk both names are one file: the old spelling must not be deleted as stale
	data, err := os.ReadFile(filepath.Join(dir, "game.exe"))
	if _, serr := os.Stat(filepath.Join(dir, "Game.exe")); err != nil || string(data) != "v2" || serr != nil {
		t.Fatalf("files: %v", tree(t, dir))
	}
}

func TestApplyFileBecomesFolder(t *testing.T) {
	dir := t.TempDir()
	stage(t, dir, "game", "1", map[string]string{"data": "file", "lib/x.bin": "x", "assets/a.png": "a"})
	apply(t, dir, "game", nil)
	stage(t, dir, "game", "2", map[string]string{"data/config": "c", "lib": "now a file", "assets/a.png": "a"})
	apply(t, dir, "game", nil)
	if got := tree(t, dir); got["data/config"] != "c" || got["lib"] != "now a file" || len(got) != 4 {
		t.Fatalf("files: %v", got) // + assets/a.png and the state
	}
	// a folder with a file of the user is never moved aside to make room for a file
	os.WriteFile(filepath.Join(dir, "assets", "mine.txt"), []byte("mine"), 0o644)
	stage(t, dir, "game", "3", map[string]string{"assets": "a file now"})
	if _, err := Apply(dir, "game", nil); err == nil {
		t.Fatal("folder with a user file replaced")
	}
	got := tree(t, dir)
	if got["assets/mine.txt"] != "mine" || !Pending(dir, "game") {
		t.Fatalf("after the refused update: %v, pending %v", got, Pending(dir, "game"))
	}
	for name := range got {
		if strings.Contains(name, ".old-") {
			t.Fatalf("moved aside: %s", name)
		}
	}
}

func TestApplyOldFileStays(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs folder permissions")
	}
	dir := t.TempDir()
	stage(t, dir, "game", "1", map[string]string{"a": "1", "ro/old.bin": "old"})
	apply(t, dir, "game", nil)
	os.Chmod(filepath.Join(dir, "ro"), 0o555)
	defer os.Chmod(filepath.Join(dir, "ro"), 0o755)
	stage(t, dir, "game", "2", map[string]string{"a": "2"})
	if _, err := Apply(dir, "game", nil); err == nil {
		t.Fatal("an old file that can't be removed was dropped silently")
	}
	if s := state(t, dir, "game"); s.Version != "1" || !Pending(dir, "game") {
		t.Fatalf("state %+v, pending %v: the update must be tried again", s, Pending(dir, "game"))
	}
}

func TestApplyLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need extra rights on Windows")
	}
	dir, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, "old.bin"), []byte("theirs"), 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, "data")); err != nil {
		t.Fatal(err)
	}
	WriteState(dir, State{Component: "game", Version: "1", Files: []string{"data/old.bin"}})
	stage(t, dir, "game", "2", map[string]string{"a": "a"})
	apply(t, dir, "game", nil)
	if _, err := os.Stat(filepath.Join(outside, "old.bin")); err != nil {
		t.Fatal("old file deleted through a link")
	}
	stage(t, dir, "game", "3", map[string]string{"data/new.bin": "new"})
	if _, err := Apply(dir, "game", nil); err == nil {
		t.Fatal("file installed through a link")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.bin")); err == nil {
		t.Fatal("file written through a link")
	}
	os.WriteFile(filepath.Join(outside, "x.old-1"), nil, 0o644)
	os.WriteFile(oldListPath(dir), []byte("data/x.old-1\n"), 0o644)
	Cleanup(dir)
	if _, err := os.Stat(filepath.Join(outside, "x.old-1")); err != nil {
		t.Fatal("Cleanup deleted through a link")
	}
}

// TestApplyStagedForOtherVersion: an update staged for the installed v1 is dropped when a setup installed v3
// meanwhile; it is never applied over v3 (no downgrade by a stale staging).
func TestApplyStagedForOtherVersion(t *testing.T) {
	dir := t.TempDir()
	if err := WriteState(dir, State{Component: "game", Version: "1", Files: []string{"a.txt"}}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1"), 0o644)
	stage(t, dir, "game", "2", map[string]string{"a.txt": "v2"})
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v3"), 0o644) // the setup of v3
	if err := WriteState(dir, State{Component: "game", Version: "3", Files: []string{"a.txt"}}); err != nil {
		t.Fatal(err)
	}
	if Pending(dir, "game") {
		t.Error("an update staged for another installed version is pending")
	}
	applied, err := Apply(dir, "game", nil)
	if err != nil || applied {
		t.Fatalf("Apply = %v, %v", applied, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(data) != "v3" {
		t.Fatalf("a.txt = %q", data)
	}
	if st, _ := Installed(dir, "game"); st.Version != "3" {
		t.Fatalf("installed %s", st.Version)
	}
	if _, err := os.Stat(stagingDir(dir, "game")); err == nil {
		t.Error("the stale staging is still there")
	}
}

// TestApplyStagedFileMissing: a staging that lost a file (and whose target is not there either) is refused before
// anything changes and deleted, so that the next check stages the update again instead of failing at every start.
func TestApplyStagedFileMissing(t *testing.T) {
	dir := t.TempDir()
	WriteState(dir, State{Component: "game", Version: "1", Files: []string{"old.txt"}})
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("v1"), 0o644)
	stage(t, dir, "game", "2", map[string]string{"a.txt": "v2", "b.txt": "v2"})
	os.Remove(filepath.Join(stagingDir(dir, "game"), "files", "b.txt"))
	if _, err := Apply(dir, "game", nil); err == nil {
		t.Fatal("Apply of an incomplete staging succeeded")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "old.txt")); string(data) != "v1" {
		t.Fatal("the installed version was changed")
	}
	if Pending(dir, "game") {
		t.Fatal("the broken staging is still pending")
	}
	if applied, err := Apply(dir, "game", nil); applied || err != nil {
		t.Fatalf("second Apply = %v, %v", applied, err)
	}
}
