package umbra

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveE2E(t *testing.T) {
	if os.Getenv("UMBRA_GO_LIVE_E2E") != "1" {
		t.Skip("set UMBRA_GO_LIVE_E2E=1 to run live e2e")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	baseURL := liveEnv("UMBRA_E2E_BASE_URL", "http://127.0.0.1:19730")
	apiBaseURL := liveEnv("UMBRA_E2E_API_BASE_URL", strings.TrimRight(baseURL, "/")+"/api/v1")
	registrationToken := mustLiveEnv(t, "UMBRA_E2E_REGISTRATION_TOKEN")

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Jar: jar}
	tokenStore := NewMemoryTokenStore()
	deviceStore := NewMemoryDeviceStore()
	opener := &liveBrowserOpener{
		t:          t,
		http:       httpClient,
		apiBaseURL: apiBaseURL,
		username:   mustLiveEnv(t, "UMBRA_E2E_USERNAME"),
		password:   mustLiveEnv(t, "UMBRA_E2E_PASSWORD"),
	}

	client, err := New(Config{
		BaseURL:       baseURL,
		APIBaseURL:    apiBaseURL,
		ClientID:      mustLiveEnv(t, "UMBRA_E2E_CLIENT_ID"),
		RedirectURI:   liveEnv("UMBRA_E2E_REDIRECT_URI", "http://127.0.0.1:38473/auth/callback"),
		HTTPClient:    httpClient,
		TokenStore:    tokenStore,
		DeviceStore:   deviceStore,
		BrowserOpener: opener,
		DeviceRegistration: &DeviceRegistrationOptions{
			RegistrationToken: registrationToken,
			Device: DeviceMetadata{
				Name:       "Umbra Go SDK E2E",
				Platform:   "e2e",
				AppVersion: "e2e",
				OSVersion:  "e2e",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := client.Login(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if session.Token == nil || session.Token.AccessToken == "" || session.Token.RefreshToken == "" {
		t.Fatalf("expected access and refresh tokens, got %#v", session.Token)
	}

	deviceCredentials, err := deviceStore.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deviceCredentials == nil || deviceCredentials.DeviceID == "" || deviceCredentials.DeviceSecret == "" {
		t.Fatalf("expected stored device credentials, got %#v", deviceCredentials)
	}

	quota, err := client.User.Quota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if quota.QuotaBytes == 0 {
		t.Fatalf("expected non-zero quota: %#v", quota)
	}

	payload := []byte("umbra go sdk live e2e\n")
	sum := sha256.Sum256(payload)
	version := "live-" + time.Now().UTC().Format("20060102T150405")
	address := BackupAddress{
		Category: BackupCategory(liveEnv("UMBRA_E2E_BACKUP_CATEGORY", string(CategoryAsset))),
		Subject:  liveEnv("UMBRA_E2E_BACKUP_SUBJECT", "e2e"),
		Version:  version,
	}

	presign, err := client.Backup.PresignUpload(ctx, PresignUploadInput{
		Address:     address,
		FileSize:    uint64(len(payload)),
		ContentType: "text/plain",
		ContentHash: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if presign.BackupID == 0 || presign.PresignedURL == "" {
		t.Fatalf("invalid presign result: %#v", presign)
	}

	if err := putPresignedObject(ctx, httpClient, presign.PresignedURL, liveEnv("UMBRA_E2E_STORAGE_PUBLIC_ENDPOINT", ""), payload, "text/plain"); err != nil {
		t.Fatal(err)
	}

	confirmed, err := client.Backup.ConfirmUpload(ctx, BackupTarget{BackupID: presign.BackupID})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.BackupID != presign.BackupID || confirmed.SizeBytes != uint64(len(payload)) {
		t.Fatalf("unexpected confirm result: %#v", confirmed)
	}

	files, err := client.Backup.List(ctx, BackupListFilter{Category: address.Category, Subject: address.Subject})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.BackupID == presign.BackupID && file.Version == address.Version && file.SizeBytes == uint64(len(payload)) {
			return
		}
	}
	t.Fatalf("uploaded backup %d was not visible in list: %#v", presign.BackupID, files)
}

type liveBrowserOpener struct {
	t          *testing.T
	http       *http.Client
	apiBaseURL string
	username   string
	password   string
}

func (o *liveBrowserOpener) OpenURL(ctx context.Context, authorizeURL string) error {
	o.t.Helper()

	nextURL, err := o.redirectLocation(ctx, authorizeURL)
	if err != nil {
		return err
	}
	loginChallenge := queryValue(nextURL, "login_challenge")
	if loginChallenge == "" {
		o.t.Fatalf("authorization redirect missing login_challenge: %s", nextURL)
	}

	if err := o.getHydraLoginInfo(ctx, loginChallenge); err != nil {
		return err
	}
	redirectTo, err := o.postHydraLogin(ctx, loginChallenge)
	if err != nil {
		return err
	}

	nextURL, err = o.redirectLocation(ctx, redirectTo)
	if err != nil {
		return err
	}
	consentChallenge := queryValue(nextURL, "consent_challenge")
	if consentChallenge == "" {
		o.t.Fatalf("authorization redirect missing consent_challenge: %s", nextURL)
	}

	redirectTo, err = o.postHydraConsent(ctx, consentChallenge)
	if err != nil {
		return err
	}
	callbackURL, err := o.redirectLocation(ctx, redirectTo)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, callbackURL, nil)
	if err != nil {
		return err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (o *liveBrowserOpener) getHydraLoginInfo(ctx context.Context, challenge string) error {
	var out struct {
		Challenge string `json:"challenge"`
	}
	return o.getEnvelope(ctx, "/auth/hydra/login?login_challenge="+url.QueryEscape(challenge), &out)
}

func (o *liveBrowserOpener) postHydraLogin(ctx context.Context, challenge string) (string, error) {
	var out struct {
		RedirectTo string `json:"redirect_to"`
	}
	err := o.postEnvelope(ctx, "/auth/hydra/login?login_challenge="+url.QueryEscape(challenge), map[string]string{
		"username": o.username,
		"password": o.password,
	}, &out)
	return out.RedirectTo, err
}

func (o *liveBrowserOpener) postHydraConsent(ctx context.Context, challenge string) (string, error) {
	var out struct {
		RedirectTo string `json:"redirect_to"`
	}
	err := o.postEnvelope(ctx, "/auth/hydra/consent?consent_challenge="+url.QueryEscape(challenge), map[string]string{}, &out)
	return out.RedirectTo, err
}

func (o *liveBrowserOpener) redirectLocation(ctx context.Context, target string) (string, error) {
	client := *o.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	location := resp.Header.Get("Location")
	if location == "" {
		o.t.Fatalf("expected redirect location from %s, status=%d", target, resp.StatusCode)
	}
	base, _ := url.Parse(target)
	parsed, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(parsed).String(), nil
}

func (o *liveBrowserOpener) getEnvelope(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(o.apiBaseURL, path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeLiveEnvelope(resp, out)
}

func (o *liveBrowserOpener) postEnvelope(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(o.apiBaseURL, path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeLiveEnvelope(resp, out)
}

func decodeLiveEnvelope(resp *http.Response, out any) error {
	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || env.Code != 0 {
		return apiError(resp.StatusCode, env.Code, env.Msg)
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func putPresignedObject(ctx context.Context, client *http.Client, presignedURL, publicEndpoint string, body []byte, contentType string) error {
	target, err := url.Parse(presignedURL)
	if err != nil {
		return err
	}
	originalHost := target.Host
	if strings.TrimSpace(publicEndpoint) != "" {
		publicURL, err := url.Parse(publicEndpoint)
		if err != nil {
			return err
		}
		target.Scheme = publicURL.Scheme
		target.Host = publicURL.Host
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(body))
	if originalHost != target.Host {
		req.Host = originalHost
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &UmbraError{Kind: ErrStorageUnavailable, HTTPStatus: resp.StatusCode, Message: "object storage upload failed"}
	}
	return nil
}

func queryValue(rawURL, key string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get(key)
}

func mustLiveEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required", key)
	}
	return value
}

func liveEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
