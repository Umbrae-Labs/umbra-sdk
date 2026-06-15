//go:build windows

package umbra

import "testing"

func TestParseRegQueryValue(t *testing.T) {
	output := `
HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion
    ProductName    REG_SZ    Windows 11 Pro
`
	if got := parseRegQueryValue(output, "ProductName"); got != "Windows 11 Pro" {
		t.Fatalf("parseRegQueryValue() = %q", got)
	}
}

func TestWindowsOSVersion(t *testing.T) {
	got := windowsOSVersion(map[string]string{
		"ProductName":        "Windows 11 Pro",
		"DisplayVersion":     "23H2",
		"CurrentBuildNumber": "22631",
		"UBR":                "3593",
	})
	if got != "Windows 11 Pro 23H2 build 22631.3593" {
		t.Fatalf("windowsOSVersion() = %q", got)
	}
}
