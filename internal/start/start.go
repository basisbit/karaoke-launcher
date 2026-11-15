// Package start runs the program from launcher.json (the game) without a shell and reports when it ends.
package start

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// Resolve returns the arguments with the program path made usable: a relative program that exists in appdir is
// taken from there; anything else is left to the system's search path.
func Resolve(args []string, appdir string) []string {
	if len(args) == 0 || filepath.IsAbs(args[0]) {
		return args
	}
	p := filepath.Join(appdir, filepath.FromSlash(args[0]))
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		out := append([]string{p}, args[1:]...)
		return out
	}
	return args
}

// Process is a started program.
type Process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

// Start runs the program. The working folder is the program's folder if the program is given as a path,
// otherwise appdir. Its output is discarded.
func Start(args []string, appdir string) (*Process, error) {
	args = Resolve(args, appdir)
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("no program to start")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = appdir
	if filepath.IsAbs(args[0]) {
		cmd.Dir = filepath.Dir(args[0])
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Process{cmd: cmd, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// Done is closed when the program has ended.
func (p *Process) Done() <-chan struct{} { return p.done }

// Wait waits until the program has ended and returns its error (nil for exit code 0).
func (p *Process) Wait() error {
	<-p.done
	return p.err
}
