//go:build windows

package umbra

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const windowsCurrentVersionKey = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`

// DetectWindowsDeviceMetadata collects Windows metadata suitable for device
// registration display and audit fields.
func DetectWindowsDeviceMetadata(options WindowsDeviceMetadataOptions) (DeviceMetadata, error) {
	name, _ := os.Hostname()
	registry := map[string]string{}
	for _, value := range []string{"ProductName", "DisplayVersion", "CurrentBuildNumber", "UBR", "EditionID"} {
		if got, err := readWindowsRegistryValue(windowsCurrentVersionKey, value); err == nil && got != "" {
			registry[value] = got
		}
	}

	metadata := cloneMetadata(options.Metadata)
	if installID := strings.TrimSpace(options.InstallID); installID != "" {
		metadata["install_id"] = installID
	} else if installID, err := loadOrCreateInstallID(options.InstallIDPath); err != nil {
		return DeviceMetadata{}, err
	} else if installID != "" {
		metadata["install_id"] = installID
	}
	if !options.SkipMachineGUIDHash {
		if machineGUID, err := readWindowsRegistryValue(`HKLM\SOFTWARE\Microsoft\Cryptography`, "MachineGuid"); err == nil && machineGUID != "" {
			metadata["machine_guid_hash"] = hashWindowsMachineGUID(machineGUID, options.MachineGUIDHashSalt)
		}
	}
	if len(registry) > 0 {
		metadata["windows"] = map[string]any{
			"product_name":    registry["ProductName"],
			"display_version": registry["DisplayVersion"],
			"build":           registry["CurrentBuildNumber"],
			"ubr":             registry["UBR"],
			"edition_id":      registry["EditionID"],
		}
	}

	return DeviceMetadata{
		Name:          strings.TrimSpace(name),
		Platform:      "windows-" + runtime.GOARCH,
		OSVersion:     windowsOSVersion(registry),
		AppVersion:    strings.TrimSpace(options.AppVersion),
		Metadata:      metadata,
		autoCollected: true,
	}, nil
}

func readWindowsRegistryValue(key, value string) (string, error) {
	out, err := newWindowsRegistryCommand(key, value).Output()
	if err != nil {
		return "", err
	}
	return parseRegQueryValue(string(out), value), nil
}

func newWindowsRegistryCommand(key, value string) *exec.Cmd {
	cmd := exec.Command("reg", "query", key, "/v", value)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func parseRegQueryValue(output, value string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), strings.ToLower(value)) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return ""
		}
		return strings.TrimSpace(strings.Join(fields[2:], " "))
	}
	return ""
}

func windowsOSVersion(values map[string]string) string {
	product := strings.TrimSpace(values["ProductName"])
	displayVersion := strings.TrimSpace(values["DisplayVersion"])
	build := strings.TrimSpace(values["CurrentBuildNumber"])
	ubr := strings.TrimSpace(values["UBR"])
	if product == "" {
		product = "Windows"
	}
	version := product
	if displayVersion != "" {
		version += " " + displayVersion
	}
	if build != "" {
		version += " build " + build
		if ubr != "" {
			if _, err := strconv.Atoi(ubr); err == nil {
				version += "." + ubr
			}
		}
	}
	return strings.TrimSpace(version)
}

func hashWindowsMachineGUID(machineGUID, salt string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(salt) + strings.TrimSpace(machineGUID)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func cloneMetadata(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+3)
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func unsupportedWindowsMetadataError() error {
	return fmt.Errorf("windows metadata detection is only supported on windows")
}
