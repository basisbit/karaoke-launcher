package update

import (
	"os"
	"os/exec"
)

// Relaunch starts the launcher again with the same arguments and does not wait for it. The caller must release
// the single-instance lock first and then exit.
func Relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
