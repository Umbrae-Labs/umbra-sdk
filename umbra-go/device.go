package umbra

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	deviceSignatureVersion  = "v1"
	registrationTokenPrefix = "umbra_reg_v1_"
	registrationDeviceID    = "registration"

	headerDeviceID   = "X-Umbra-Device-Id"
	headerTimestamp  = "X-Umbra-Timestamp"
	headerNonce      = "X-Umbra-Nonce"
	headerBodySHA256 = "X-Umbra-Body-SHA256"
	headerSignature  = "X-Umbra-Signature"
)

type DeviceClient struct {
	api                 *apiClient
	store               DeviceStore
	defaultRegistration *DeviceRegistrationOptions
}

type DeviceCredentials struct {
	DeviceID     string `json:"device_id"`
	DeviceSecret string `json:"device_secret"`
}

type DeviceMetadata struct {
	Name        string         `json:"name"`
	Platform    string         `json:"platform,omitempty"`
	AppVersion  string         `json:"app_version,omitempty"`
	OSVersion   string         `json:"os_version,omitempty"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`

	autoCollected bool
}

type DeviceRegistrationOptions struct {
	CredentialID      string
	CredentialSecret  string
	RegistrationToken string
	Device            DeviceMetadata
}

type Device struct {
	ID                        uint64         `json:"id"`
	DeviceID                  string         `json:"device_id"`
	UserID                    uint64         `json:"user_id"`
	TenantID                  *uint64        `json:"tenant_id,omitempty"`
	ClientID                  string         `json:"client_id"`
	DistributionCredentialID  uint64         `json:"distribution_credential_id"`
	DistributionCredentialKey string         `json:"distribution_credential_key,omitempty"`
	Name                      string         `json:"name"`
	Platform                  string         `json:"platform"`
	AppVersion                string         `json:"app_version"`
	OSVersion                 string         `json:"os_version"`
	Fingerprint               string         `json:"fingerprint,omitempty"`
	Metadata                  map[string]any `json:"metadata,omitempty"`
	Status                    uint8          `json:"status"`
	CreatedAt                 time.Time      `json:"created_at"`
	UpdatedAt                 time.Time      `json:"updated_at"`
	LastActiveAt              *time.Time     `json:"last_active_at,omitempty"`
	RotatedAt                 *time.Time     `json:"rotated_at,omitempty"`
	RevokedAt                 *time.Time     `json:"revoked_at,omitempty"`
}

type DeviceRegistrationResult struct {
	Device       Device `json:"device"`
	DeviceSecret string `json:"device_secret,omitempty"`
	SecretOnce   bool   `json:"secret_once"`
}

type deviceRegistrationRequest struct {
	CredentialID      string         `json:"credential_id,omitempty"`
	RegistrationToken string         `json:"registration_token,omitempty"`
	Device            DeviceMetadata `json:"device"`
}

// Register registers the current authenticated user device and stores the
// returned one-time device_secret.
func (d *DeviceClient) Register(ctx context.Context, options DeviceRegistrationOptions) (*DeviceRegistrationResult, error) {
	credentialID, credentialSecret, err := resolveRegistrationSecret(options)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.Device.Name) == "" {
		return nil, invalidInput("device name is required")
	}
	if !options.Device.autoCollected {
		return nil, invalidInput("device metadata must be collected by the SDK")
	}
	requestCredentialID := strings.TrimSpace(options.CredentialID)
	if strings.TrimSpace(options.RegistrationToken) == "" {
		requestCredentialID = credentialID
	}
	body := deviceRegistrationRequest{
		CredentialID:      requestCredentialID,
		RegistrationToken: strings.TrimSpace(options.RegistrationToken),
		Device:            options.Device,
	}
	var out DeviceRegistrationResult
	if err := d.api.postSigned(ctx, "/client/devices/register", body, &out, requestSigner{
		Secret:   credentialSecret,
		DeviceID: registrationDeviceID,
	}); err != nil {
		return nil, err
	}
	if out.Device.DeviceID == "" || out.DeviceSecret == "" {
		return nil, invalidInput("device registration response missing credentials")
	}
	if d.store != nil {
		if err := d.store.Save(ctx, &DeviceCredentials{DeviceID: out.Device.DeviceID, DeviceSecret: out.DeviceSecret}); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// EnsureRegistered returns stored device credentials or registers the device
// using the supplied options when no credentials are stored.
func (d *DeviceClient) EnsureRegistered(ctx context.Context, options DeviceRegistrationOptions) (*DeviceCredentials, error) {
	if d.store != nil {
		credentials, err := d.store.Load(ctx)
		if err != nil {
			return nil, err
		}
		if credentials != nil && credentials.DeviceID != "" && credentials.DeviceSecret != "" {
			return credentials, nil
		}
	}
	result, err := d.Register(ctx, options)
	if err != nil {
		return nil, err
	}
	return &DeviceCredentials{DeviceID: result.Device.DeviceID, DeviceSecret: result.DeviceSecret}, nil
}

// RegisterDefault registers with Config.DeviceRegistration.
func (d *DeviceClient) RegisterDefault(ctx context.Context) (*DeviceRegistrationResult, error) {
	if d.defaultRegistration == nil {
		return nil, invalidInput("device registration is not configured")
	}
	return d.Register(ctx, *d.defaultRegistration)
}

// EnsureDefaultRegistered ensures a device is registered with Config.DeviceRegistration.
func (d *DeviceClient) EnsureDefaultRegistered(ctx context.Context) (*DeviceCredentials, error) {
	if d.defaultRegistration == nil {
		return nil, invalidInput("device registration is not configured")
	}
	return d.EnsureRegistered(ctx, *d.defaultRegistration)
}

// RotateSecret rotates the current user's device secret and stores the new
// secret when the rotated device matches the local credentials.
func (d *DeviceClient) RotateSecret(ctx context.Context, deviceID string) (*DeviceRegistrationResult, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" && d.store != nil {
		credentials, err := d.store.Load(ctx)
		if err != nil {
			return nil, err
		}
		if credentials != nil {
			deviceID = credentials.DeviceID
		}
	}
	if deviceID == "" {
		return nil, invalidInput("device id is required")
	}
	var out DeviceRegistrationResult
	if err := d.api.post(ctx, "/user/devices/"+url.PathEscape(deviceID)+"/rotate-secret", map[string]any{}, &out); err != nil {
		return nil, err
	}
	if out.Device.DeviceID == "" || out.DeviceSecret == "" {
		return nil, invalidInput("device secret rotation response missing credentials")
	}
	if d.store != nil {
		if err := d.store.Save(ctx, &DeviceCredentials{DeviceID: out.Device.DeviceID, DeviceSecret: out.DeviceSecret}); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

func resolveRegistrationSecret(options DeviceRegistrationOptions) (string, string, error) {
	credentialID := strings.TrimSpace(options.CredentialID)
	credentialSecret := strings.TrimSpace(options.CredentialSecret)
	registrationToken := strings.TrimSpace(options.RegistrationToken)
	if registrationToken != "" {
		tokenCredentialID, tokenSecret, err := ParseRegistrationToken(registrationToken)
		if err != nil {
			return "", "", err
		}
		if credentialID != "" && credentialID != tokenCredentialID {
			return "", "", invalidInput("credential id does not match registration token")
		}
		credentialID = tokenCredentialID
		credentialSecret = tokenSecret
	}
	if credentialID == "" || credentialSecret == "" {
		return "", "", invalidInput("registration credential id and secret are required")
	}
	return credentialID, credentialSecret, nil
}

func MakeRegistrationToken(credentialID, credentialSecret string) (string, error) {
	credentialID = strings.TrimSpace(credentialID)
	credentialSecret = strings.TrimSpace(credentialSecret)
	if credentialID == "" || credentialSecret == "" || strings.Contains(credentialID, ".") || strings.Contains(credentialSecret, ".") {
		return "", invalidInput("registration credential id and secret are required")
	}
	return registrationTokenPrefix + credentialID + "." + credentialSecret, nil
}

func ParseRegistrationToken(token string) (string, string, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, registrationTokenPrefix) {
		return "", "", invalidInput("registration token is invalid")
	}
	payload := strings.TrimPrefix(token, registrationTokenPrefix)
	credentialID, credentialSecret, ok := strings.Cut(payload, ".")
	credentialID = strings.TrimSpace(credentialID)
	credentialSecret = strings.TrimSpace(credentialSecret)
	if !ok || credentialID == "" || credentialSecret == "" || strings.Contains(credentialSecret, ".") {
		return "", "", invalidInput("registration token is invalid")
	}
	return credentialID, credentialSecret, nil
}

type requestSigner struct {
	Secret           string
	DeviceID         string
	SkipDeviceHeader bool
}

func signRequest(req *http.Request, body []byte, signer requestSigner) error {
	secret := strings.TrimSpace(signer.Secret)
	deviceID := strings.TrimSpace(signer.DeviceID)
	if secret == "" || deviceID == "" {
		return invalidInput("device signing credentials are required")
	}
	timestamp := time.Now().Unix()
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	bodyHash := BodySHA256Base64URL(body)
	canonical := CanonicalString(req.Method, canonicalPathWithQuery(req.URL), timestamp, nonce, bodyHash, deviceID)
	signature := SignDeviceRequest(secret, canonical)
	if !signer.SkipDeviceHeader && deviceID != registrationDeviceID {
		req.Header.Set(headerDeviceID, deviceID)
	}
	req.Header.Set(headerTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(headerNonce, nonce)
	req.Header.Set(headerBodySHA256, bodyHash)
	req.Header.Set(headerSignature, deviceSignatureVersion+"="+signature)
	return nil
}

func CanonicalString(method, pathWithQuery string, timestamp int64, nonce, bodyHash, deviceID string) string {
	return strings.Join([]string{
		deviceSignatureVersion,
		strings.ToUpper(strings.TrimSpace(method)),
		pathWithQuery,
		strconv.FormatInt(timestamp, 10),
		strings.TrimSpace(nonce),
		strings.TrimSpace(bodyHash),
		strings.TrimSpace(deviceID),
	}, "\n")
}

func BodySHA256Base64URL(body []byte) string {
	sum := sha256.Sum256(body)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func SignDeviceRequest(secret string, canonical string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func canonicalPathWithQuery(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		return path + "?" + u.RawQuery
	}
	return path
}

func newNonce() (string, error) {
	buf := make([]byte, 18)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
