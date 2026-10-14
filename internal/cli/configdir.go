package cli

import (
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDir returns the user config dir: $XDG_CONFIG_HOME, else ~/.config
// (on macOS too, rather than ~/Library/Application Support), else the OS
// default on Windows.
func ConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(d) {
		return d
	}
	if runtime.GOOS != "windows" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config")
		}
	}
	d, _ := os.UserConfigDir()
	return d
}
