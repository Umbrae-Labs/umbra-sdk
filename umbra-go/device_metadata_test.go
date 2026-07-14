package umbra

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsDeviceFingerprintIsStable(t *testing.T) {
	const want = "windows:v1:068442b331fed45178be4b7e7a403f261b19e55ff789340babc97e60cdcb414f"
	for _, machineGUID := range []string{
		"4C4C4544-0038-3710-8051-CAC04F4B4332",
		" {4c4c4544-0038-3710-8051-cac04f4b4332} ",
	} {
		if got := windowsDeviceFingerprint(machineGUID); got != want {
			t.Fatalf("windowsDeviceFingerprint(%q) = %q, want %q", machineGUID, got, want)
		}
	}
}

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
