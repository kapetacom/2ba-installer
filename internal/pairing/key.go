package pairing

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultKeyPath is where the installer persists the API key:
// ~/.config/2ba/2BA_API_KEY (mode 0600, no trailing newline).
func DefaultKeyPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "2ba", "2BA_API_KEY")
}

// SaveKey writes key to path (creating parent dirs) with 0600 permissions and
// no trailing newline.
func SaveKey(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return err
	}
	// os.WriteFile honours the process umask; force the intended 0600.
	return os.Chmod(path, 0o600)
}

// LoadKey reads and trims the key from path.
func LoadKey(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// KeyExists reports whether path exists.
func KeyExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
