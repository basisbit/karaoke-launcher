package update

import (
	"os"
	"strconv"
	"strings"
)

// processPaths returns the executable of every process the user may look at (/proc/<pid>/exe).
func processPaths() ([]string, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	var exes []string
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == selfPID {
			continue
		}
		exe, err := os.Readlink("/proc/" + e.Name() + "/exe")
		if err != nil {
			continue // another user's process, or it ended
		}
		exes = append(exes, strings.TrimSuffix(exe, " (deleted)")) // replaced by an update while it runs
	}
	return exes, true
}
