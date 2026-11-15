// Command game is the dummy game of the example bundle. It is no real game: it writes its version and its
// arguments into game-run.txt next to itself and exits. make-game.sh builds and packs it.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// version is set when it is built: -ldflags "-X main.version=1.0.0".
var version = "dev"

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	text := "version " + version + "\n" + strings.Join(os.Args[1:], "\n") + "\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(exe), "game-run.txt"), []byte(text), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
