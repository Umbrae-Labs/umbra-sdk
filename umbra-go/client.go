package umbra

import (
	"context"
	"net/http"
)

// Client is the root Umbra SDK client.
type Client struct {
	Auth    *AuthClient
	User    *UserClient
	Backup  *BackupClient
	Devices *DeviceClient

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
	return c, nil
}

// Login opens the browser, completes OAuth login, and registers the device when
// Config.DeviceRegistration is set and no device credentials are stored yet.
func (c *Client) Login(ctx context.Context) (*Session, error) {
	session, err := c.Auth.Login(ctx)
	if err != nil {
		return nil, err
	}
	if c.config.DeviceRegistration != nil {
		if _, err := c.Devices.EnsureRegistered(ctx, *c.config.DeviceRegistration); err != nil {
			return nil, err
		}
	}
	return session, nil
}

// Logout revokes OAuth tokens where possible and clears local token and device
// credentials.
func (c *Client) Logout(ctx context.Context) error {
	err := c.Auth.Logout(ctx)
	if clearErr := c.config.DeviceStore.Clear(ctx); err == nil {
		err = clearErr
	}
	return err
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
