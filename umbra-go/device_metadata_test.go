package umbra

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateInstallIDPersistsStableValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install_id")
	first, err := loadOrCreateInstallID(path)
	if err != nil {
		t.Fatalf("loadOrCreateInstallID() error = %v", err)
	}
	if first == "" {
		t.Fatal("install id is empty")
	}
	second, err := loadOrCreateInstallID(path)
	if err != nil {
		t.Fatalf("loadOrCreateInstallID() second error = %v", err)
	}
	if second != first {
		t.Fatalf("install id changed: %q != %q", second, first)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != first+"\n" {
		t.Fatalf("stored install id = %q", string(data))
	}
}
