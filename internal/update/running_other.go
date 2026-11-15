//go:build !linux && !windows

package update

// processPaths can't list the running programs on this system (macOS and others): the user's answer counts.
func processPaths() ([]string, bool) { return nil, false }
