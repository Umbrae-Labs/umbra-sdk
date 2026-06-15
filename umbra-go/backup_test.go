package umbra

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUploadReaderPresignPutConfirm(t *testing.T) {
	var objectURL string
	var putBody string

	objectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("object method = %s", r.Method)
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		putBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer objectServer.Close()
	objectURL = objectServer.URL + "/object"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/client/backup/presign", func(w http.ResponseWriter, r *http.Request) {
		var req presignUploadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Category != "game" || req.Subject != "mc" || req.Version != "v1" {
			t.Fatalf("request address = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": PresignUploadResult{BackupID: 42, PresignedURL: objectURL, ExpiresIn: 3600},
		})
	})
	mux.HandleFunc("/api/v1/client/backup/confirm", func(w http.ResponseWriter, r *http.Request) {
		var req backupTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.BackupID != 42 {
			t.Fatalf("backup_id = %d, want 42", req.BackupID)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": ConfirmUploadResult{
				BackupID:  42,
				SizeBytes: 5,
				ETag:      "etag",
				Quota:     QuotaInfo{QuotaBytes: 10, UsedBytes: 5, AvailableBytes: 5},
			},
		})
	})
	apiServer := httptest.NewServer(mux)
	defer apiServer.Close()

	store := NewMemoryTokenStore()
	if err := store.Save(context.Background(), &TokenSet{
		AccessToken: "token",
		TokenType:   "bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	deviceStore := NewMemoryDeviceStore()
	if err := deviceStore.Save(context.Background(), &DeviceCredentials{DeviceID: "dev_test", DeviceSecret: "secret"}); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		BaseURL:     apiServer.URL,
		ClientID:    "client",
		RedirectURI: "http://127.0.0.1:1420/auth/callback",
		TokenStore:  store,
		DeviceStore: deviceStore,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Backup.UploadReader(
		context.Background(),
		GameBackup("mc", "v1"),
		strings.NewReader("hello"),
		5,
		UploadOptions{ContentType: "text/plain"},
	)
	if err != nil {
		t.Fatalf("UploadReader() error = %v", err)
	}
	if result.BackupID != 42 || result.SizeBytes != 5 {
		t.Fatalf("result = %+v", result)
	}
	if putBody != "hello" {
		t.Fatalf("putBody = %q, want hello", putBody)
	}
}
