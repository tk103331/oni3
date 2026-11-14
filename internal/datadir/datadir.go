// Package datadir resolves the per-user directory that holds the CLI's
// configuration, skills, sessions, logs and state.
package datadir

import (
	"os"
	"path/filepath"
)

// name is the user data directory name.
const name = ".oni3"

// Dir returns the user data directory (~/.oni3), or "" when the home
// directory cannot be determined.
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, name)
}
