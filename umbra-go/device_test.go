package umbra

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDeviceSigningVector(t *testing.T) {
	bodyHash := BodySHA256Base64URL([]byte(`{"name":"LunaBox"}`))
	if bodyHash != "njjTtBgg9nmsDBrctFvuSK8L6lsXJW7eeRFtarJC20M" {
		t.Fatalf("body hash = %q", bodyHash)
	}
	canonical := CanonicalString(
		"post",
		"/api/v1/client/backup/presign?category=world&version=42",
		1716200000,
		"nonce-001",
		bodyHash,
		"dev_test_123",
	)
	wantCanonical := strings.Join([]string{
		"v1",
		"POST",
		"/api/v1/client/backup/presign?category=world&version=42",
		"1716200000",
		"nonce-001",
		"njjTtBgg9nmsDBrctFvuSK8L6lsXJW7eeRFtarJC20M",
		"dev_test_123",
	}, "\n")
	if canonical != wantCanonical {
		t.Fatalf("canonical = %q, want %q", canonical, wantCanonical)
	}
	signature := SignDeviceRequest("test-device-secret", canonical)
	if signature != "HGr3hoz1CHqufCk3xd43kwfHBi3XhTMTtfKpVy2POZA" {
		t.Fatalf("signature = %q", signature)
	}
}

func TestBackupRequestIsSigned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/client/backup/presign", func(w http.ResponseWriter, r *http.Request) {
		var req presignUploadRequest
		raw := decodeJSONBody(t, r, &req)
		if req.Category != "game" {
			t.Fatalf("category = %q", req.Category)
		}
		assertSignedRequest(t, r, raw, "dev_test_123", "test-device-secret")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": PresignUploadResult{BackupID: 7, PresignedURL: "http://127.0.0.1/object", ExpiresIn: 60},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newSignedTestClient(t, server.URL, "dev_test_123", "test-device-secret")
	out, err := client.Backup.PresignUpload(context.Background(), PresignUploadInput{
		Address:     GameBackup("world", "v42"),
		FileSize:    10,
		ContentType: "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("PresignUpload() error = %v", err)
	}
	if out.BackupID != 7 {
		t.Fatalf("BackupID = %d, want 7", out.BackupID)
	}
}

func TestDeviceRegisterSignsAndStoresCredentials(t *testing.T) {
	const credentialID = "ucd_test"
	const credentialSecret = "credential-secret"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/client/devices/register", func(w http.ResponseWriter, r *http.Request) {
		var req deviceRegistrationRequest
		raw := decodeJSONBody(t, r, &req)
		if req.CredentialID != credentialID {
			t.Fatalf("credential_id = %q", req.CredentialID)
		}
		if req.Device.Name != "LunaBook" {
			t.Fatalf("device name = %q", req.Device.Name)
		}
		if r.Header.Get(headerDeviceID) != "" {
			t.Fatalf("registration sent device id header %q", r.Header.Get(headerDeviceID))
		}
		assertSignedRequest(t, r, raw, registrationDeviceID, credentialSecret)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": DeviceRegistrationResult{
				Device:       Device{DeviceID: "dev_registered", Name: "LunaBook"},
				DeviceSecret: "device-secret",
				SecretOnce:   true,
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tokenStore := NewMemoryTokenStore()
	if err := tokenStore.Save(context.Background(), &TokenSet{
		AccessToken: "token",
		TokenType:   "bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	deviceStore := NewMemoryDeviceStore()
	client, err := New(Config{
		BaseURL:     server.URL,
		ClientID:    "client",
		RedirectURI: "http://127.0.0.1:1420/auth/callback",
		TokenStore:  tokenStore,
		DeviceStore: deviceStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Devices.Register(context.Background(), DeviceRegistrationOptions{
		CredentialID:     credentialID,
		CredentialSecret: credentialSecret,
		Device:           DeviceMetadata{Name: "LunaBook", Platform: "darwin"},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.Device.DeviceID != "dev_registered" {
		t.Fatalf("registered device id = %q", result.Device.DeviceID)
	}
	stored, err := deviceStore.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.DeviceID != "dev_registered" || stored.DeviceSecret != "device-secret" {
		t.Fatalf("stored credentials = %+v", stored)
	}
}

func decodeJSONBody[T any](t *testing.T, r *http.Request, out *T) []byte {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertSignedRequest(t *testing.T, r *http.Request, body []byte, deviceID, secret string) {
	t.Helper()
	timestamp := r.Header.Get(headerTimestamp)
	nonce := r.Header.Get(headerNonce)
	bodyHash := r.Header.Get(headerBodySHA256)
	signature := strings.TrimPrefix(r.Header.Get(headerSignature), "v1=")
	if timestamp == "" || nonce == "" || bodyHash == "" || signature == "" {
		t.Fatalf("missing signature headers: timestamp=%q nonce=%q body_hash=%q signature=%q", timestamp, nonce, bodyHash, signature)
	}
	if bodyHash != BodySHA256Base64URL(body) {
		t.Fatalf("body hash = %q, want %q", bodyHash, BodySHA256Base64URL(body))
	}
	parsedTimestamp, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	canonical := CanonicalString(r.Method, canonicalPathWithQuery(r.URL), parsedTimestamp, nonce, bodyHash, deviceID)
	want := SignDeviceRequest(secret, canonical)
	if !hmac.Equal([]byte(signature), []byte(want)) {
		t.Fatalf("signature = %q, want %q", signature, want)
	}
}

func newSignedTestClient(t *testing.T, baseURL, deviceID, deviceSecret string) *Client {
	t.Helper()
	tokenStore := NewMemoryTokenStore()
	if err := tokenStore.Save(context.Background(), &TokenSet{
		AccessToken: "token",
		TokenType:   "bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	deviceStore := NewMemoryDeviceStore()
	if err := deviceStore.Save(context.Background(), &DeviceCredentials{DeviceID: deviceID, DeviceSecret: deviceSecret}); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		BaseURL:     baseURL,
		ClientID:    "client",
		RedirectURI: "http://127.0.0.1:1420/auth/callback",
		TokenStore:  tokenStore,
		DeviceStore: deviceStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
