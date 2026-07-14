package umbra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestLogoutReportsDeviceOfflineAndClearsCredentials(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/client/devices/logout", func(w http.ResponseWriter, r *http.Request) {
		assertSignedRequest(t, r, []byte("{}"), "dev_stable", "device-secret")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": Device{DeviceID: "dev_stable", Status: 1},
		})
	})
	mux.HandleFunc("/oauth2/revoke", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	tokenStore := NewMemoryTokenStore()
	if err := tokenStore.Save(ctx, &TokenSet{
		AccessToken: "access-token",
		TokenType:   "bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	deviceStore := NewMemoryDeviceStore()
	if err := deviceStore.Save(ctx, &DeviceCredentials{DeviceID: "dev_stable", DeviceSecret: "device-secret"}); err != nil {
		t.Fatal(err)
	}

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
	if err := client.Logout(ctx); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	if token, err := tokenStore.Load(ctx); err != nil {
		t.Fatal(err)
	} else if token != nil {
		t.Fatalf("stored token = %+v, want nil", token)
	}
	if got, err := deviceStore.Load(ctx); err != nil {
		t.Fatal(err)
	} else if got != nil {
		t.Fatalf("stored device credentials = %+v, want nil", got)
	}
}

func TestLoginAfterLogoutReusesRegisteredDevice(t *testing.T) {
	var registrationCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-token",
			"token_type":   "bearer",
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/oauth2/revoke", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/v1/client/devices/register", func(w http.ResponseWriter, r *http.Request) {
		registrationCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": DeviceRegistrationResult{
				Device:       Device{DeviceID: "dev_stable", Name: "LunaBook"},
				DeviceSecret: "device-secret",
				SecretOnce:   true,
			},
		})
	})
	mux.HandleFunc("/api/v1/client/devices/logout", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": Device{DeviceID: "dev_stable", Status: 1},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	deviceStore := NewMemoryDeviceStore()
	client, err := New(Config{
		BaseURL:          server.URL,
		ClientID:         "client",
		RedirectURI:      "http://127.0.0.1:1420/auth/callback",
		TokenStore:       NewMemoryTokenStore(),
		DeviceStore:      deviceStore,
		BrowserOpener:    NoopBrowserOpener{},
		CallbackReceiver: staticCallbackReceiver{},
		DeviceRegistration: &DeviceRegistrationOptions{
			CredentialID:     "ucd_test",
			CredentialSecret: "registration-secret",
			Device: DeviceMetadata{
				Name:          "LunaBook",
				Platform:      "windows-amd64",
				autoCollected: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if _, err := client.Login(ctx); err != nil {
		t.Fatalf("first Login() error = %v", err)
	}
	if err := client.Logout(ctx); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := client.Login(ctx); err != nil {
		t.Fatalf("second Login() error = %v", err)
	}

	if got := registrationCalls.Load(); got != 2 {
		t.Fatalf("device registration calls = %d, want 2", got)
	}
	if got, err := deviceStore.Load(ctx); err != nil {
		t.Fatal(err)
	} else if got == nil || got.DeviceID != "dev_stable" || got.DeviceSecret != "device-secret" {
		t.Fatalf("stored device credentials = %+v", got)
	}
}

type staticCallbackReceiver struct{}

func (staticCallbackReceiver) Receive(_ context.Context, expectedState string) (*AuthCallback, error) {
	return &AuthCallback{Code: "authorization-code", State: expectedState}, nil
}
