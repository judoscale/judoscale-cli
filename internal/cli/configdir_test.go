package cli

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigDirHonorsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got := ConfigDir(); got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
}

func TestConfigDirIgnoresRelativeXDG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the OS default on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if got, want := ConfigDir(), filepath.Join(home, ".config"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestConfigDirDefaultsToDotConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the OS default on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if got, want := ConfigDir(), filepath.Join(home, ".config"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
