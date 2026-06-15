package umbra

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// WindowsDeviceMetadataOptions configures automatic Windows device metadata
// detection. InstallIDPath is optional; when set, the SDK loads or creates a
// stable random install_id at that path.
type WindowsDeviceMetadataOptions struct {
	AppVersion          string
	InstallID           string
	InstallIDPath       string
	MachineGUIDHashSalt string
	SkipMachineGUIDHash bool
	Metadata            map[string]any
}

func loadOrCreateInstallID(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	id, err := newInstallID()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && filepath.Dir(path) != "." {
		return "", err
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func newInstallID() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
