package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const credentialsFile = "credentials"

func (a *App) credentialsPath() string {
	if a.appDir() == "" {
		return ""
	}
	return filepath.Join(a.appDir(), credentialsFile)
}

// token returns the API token and where it came from: the environment first,
// then the stored credentials file. It is empty if neither has one.
func (a *App) token() (token, source string, err error) {
	if t := a.Getenv("JUDO_API_TOKEN"); t != "" {
		return t, "JUDO_API_TOKEN", nil
	}
	path := a.credentialsPath()
	if path == "" {
		return "", "", nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("reading credentials: %w", err)
	}
	return strings.TrimSpace(string(data)), path, nil
}

// saveToken stores the token in the credentials file, readable only by the
// user. The file is replaced atomically.
func (a *App) saveToken(token string) (string, error) {
	path := a.credentialsPath()
	if path == "" {
		return "", errors.New("cannot determine the user config directory")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, credentialsFile+".*") // created 0600
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// removeToken deletes the credentials file and reports whether there was one.
func (a *App) removeToken() (bool, error) {
	path := a.credentialsPath()
	if path == "" {
		return false, nil
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
