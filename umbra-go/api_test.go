package umbra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIRefreshesAndRetriesOnInvalidToken(t *testing.T) {
	var quotaCalls int
	var tokenCalls int

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/user/quota", func(w http.ResponseWriter, r *http.Request) {
		quotaCalls++
		if r.Header.Get("Authorization") == "Bearer old-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(envelope{Code: 1004, Msg: "Token invalid"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer new-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": QuotaInfo{QuotaBytes: 10, UsedBytes: 3, AvailableBytes: 7},
		})
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Fatalf("grant_type = %q", r.Form.Get("grant_type"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-token",
			"refresh_token": "refresh-token",
			"token_type":    "bearer",
			"expires_in":    3600,
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	store := NewMemoryTokenStore()
	if err := store.Save(context.Background(), &TokenSet{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		TokenType:    "bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		BaseURL:     server.URL,
		ClientID:    "client",
		RedirectURI: "http://127.0.0.1:1420/auth/callback",
		TokenStore:  store,
	})
	if err != nil {
		t.Fatal(err)
	}

	quota, err := client.User.Quota(context.Background())
	if err != nil {
		t.Fatalf("Quota() error = %v", err)
	}
	if quota.AvailableBytes != 7 {
		t.Fatalf("AvailableBytes = %d, want 7", quota.AvailableBytes)
	}
	if quotaCalls != 2 {
		t.Fatalf("quotaCalls = %d, want 2", quotaCalls)
	}
	if tokenCalls != 1 {
		t.Fatalf("tokenCalls = %d, want 1", tokenCalls)
	}
}
