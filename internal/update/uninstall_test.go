package update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUninstall removes the listed files and empty folders and keeps what the game and the user wrote, the file named
// in except, and files that match keep (never listed).
func TestUninstall(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"game.exe", "data/a/level.txt", "data/b.txt", "themes/Deluxe.ini", "launcher.exe"} {
		write(rel, "installed")
	}
	write("config.ini", "the player's settings") // matches keep: never listed
	write("Ultrastar.db", "scores")              // written by the game
	write("playlists/party.upl", "a playlist")   // written by the game
	write("themes/Mine.ini", "added by the user")
	write(".update-game/files/game.exe", "staged")
	write("old.dll.old-1", "moved aside")
	write(".update-old.txt", "old.dll.old-1\n")
	if err := WriteState(dir, State{Component: "game", Version: "1", Files: []string{"game.exe", "data/a/level.txt",
		"data/b.txt", "themes/Deluxe.ini", "launcher.exe", "gone.txt", "../outside.txt"}}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(dir, "game", filepath.Join(dir, "launcher.exe")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"game.exe", "data", "themes/Deluxe.ini", ".update-game", "old.dll.old-1", ".update-old.txt", ".component-game.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", rel, err)
		}
	}
	for _, rel := range []string{"config.ini", "Ultrastar.db", "playlists/party.upl", "themes/Mine.ini", "launcher.exe"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s was removed: %v", rel, err)
		}
	}
	// nothing installed: nothing to do
	if err := Uninstall(dir, "game", ""); err != nil {
		t.Fatal(err)
	}
}
