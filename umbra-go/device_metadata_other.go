//go:build !windows

package umbra

// DetectWindowsDeviceMetadata is only available on Windows.
func DetectWindowsDeviceMetadata(WindowsDeviceMetadataOptions) (DeviceMetadata, error) {
	return DeviceMetadata{}, invalidInput("windows device metadata detection is only supported on windows")
}
