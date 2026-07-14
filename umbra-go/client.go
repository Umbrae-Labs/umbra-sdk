package umbra

import (
	"context"
	"errors"
	"net/http"
)

// Client is the root Umbra SDK client.
type Client struct {
	Auth    *AuthClient
	User    *UserClient
	Backup  *BackupClient
	Devices *DeviceClient
	Sync    *SyncClient

	config    Config
	endpoints endpoints
	api       *apiClient
}

// New creates a new Umbra SDK client.
func New(cfg Config) (*Client, error) {
	normalized, ep, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	c := &Client{
		config:    normalized,
		endpoints: ep,
	}
	c.Auth = &AuthClient{
		config:    normalized,
		endpoints: ep,
		http:      normalized.HTTPClient,
		store:     normalized.TokenStore,
		opener:    normalized.BrowserOpener,
		callback:  normalized.CallbackReceiver,
	}
	c.api = &apiClient{
		http:        normalized.HTTPClient,
		baseURL:     ep.apiBaseURL,
		auth:        c.Auth,
		deviceStore: normalized.DeviceStore,
		userAgent:   "umbra-go",
	}
	c.User = &UserClient{api: c.api}
	c.Devices = &DeviceClient{
		api:                 c.api,
		store:               normalized.DeviceStore,
		defaultRegistration: normalized.DeviceRegistration,
	}
	c.Backup = &BackupClient{
		api:  c.api,
		http: normalized.HTTPClient,
	}
	c.Sync = &SyncClient{api: c.api}
	return c, nil
}

// Login opens the browser, completes OAuth login, and reports the device online
// when Config.DeviceRegistration is set. Stable server records are reused.
func (c *Client) Login(ctx context.Context) (*Session, error) {
	session, err := c.Auth.Login(ctx)
	if err != nil {
		return nil, err
	}
	if c.config.DeviceRegistration != nil {
		if _, err := c.Devices.Register(ctx, *c.config.DeviceRegistration); err != nil {
			return nil, err
		}
	}
	return session, nil
}

// Logout reports the device offline, revokes OAuth tokens where possible, and
// clears both local credential stores.
func (c *Client) Logout(ctx context.Context) error {
	deviceErr := c.Devices.Logout(ctx)
	authErr := c.Auth.Logout(ctx)
	return errors.Join(deviceErr, authErr)
}

// HTTPClient returns the underlying HTTP client.
func (c *Client) HTTPClient() *http.Client {
	return c.config.HTTPClient
}

// APIBaseURL returns the API base URL used by this client.
func (c *Client) APIBaseURL() string {
	return c.endpoints.apiBaseURL
}

// AuthorizationEndpoint returns the OAuth authorization endpoint.
func (c *Client) AuthorizationEndpoint() string {
	return c.endpoints.authorizationEndpoint
}

// TokenEndpoint returns the OAuth token endpoint.
func (c *Client) TokenEndpoint() string {
	return c.endpoints.tokenEndpoint
}
