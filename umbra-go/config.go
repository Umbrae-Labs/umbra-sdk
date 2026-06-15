package umbra

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultScope       = "openid offline_access"
	defaultRefreshSkew = time.Minute
)

// Config configures an Umbra SDK client.
type Config struct {
	// BaseURL is the public Umbra origin, for example https://umbra.example.com.
	// By default the SDK derives:
	//   APIBaseURL = BaseURL + "/api/v1"
	//   AuthorizationEndpoint = BaseURL + "/oauth2/auth"
	//   TokenEndpoint = BaseURL + "/oauth2/token"
	//   RevocationEndpoint = BaseURL + "/oauth2/revoke"
	BaseURL string

	ClientID    string
	RedirectURI string
	Scope       string

	// Optional advanced endpoint overrides.
	APIBaseURL            string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RevocationEndpoint    string

	HTTPClient         *http.Client
	TokenStore         TokenStore
	DeviceStore        DeviceStore
	DeviceRegistration *DeviceRegistrationOptions
	BrowserOpener      BrowserOpener
	CallbackReceiver   CallbackReceiver

	RefreshSkew time.Duration
}

type endpoints struct {
	baseURL               string
	apiBaseURL            string
	authorizationEndpoint string
	tokenEndpoint         string
	revocationEndpoint    string
}

func normalizeConfig(cfg Config) (Config, endpoints, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.RedirectURI = strings.TrimSpace(cfg.RedirectURI)
	cfg.Scope = strings.TrimSpace(cfg.Scope)
	cfg.APIBaseURL = strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	cfg.AuthorizationEndpoint = strings.TrimSpace(cfg.AuthorizationEndpoint)
	cfg.TokenEndpoint = strings.TrimSpace(cfg.TokenEndpoint)
	cfg.RevocationEndpoint = strings.TrimSpace(cfg.RevocationEndpoint)

	if cfg.BaseURL == "" {
		return Config{}, endpoints{}, errors.New("umbra: BaseURL is required")
	}
	if _, err := parseAbsoluteURL(cfg.BaseURL); err != nil {
		return Config{}, endpoints{}, errors.New("umbra: BaseURL must be an absolute URL")
	}
	if cfg.ClientID == "" {
		return Config{}, endpoints{}, errors.New("umbra: ClientID is required")
	}
	if cfg.RedirectURI == "" && cfg.CallbackReceiver == nil {
		return Config{}, endpoints{}, errors.New("umbra: RedirectURI or CallbackReceiver is required")
	}
	if cfg.Scope == "" {
		cfg.Scope = defaultScope
	}
	if cfg.RefreshSkew <= 0 {
		cfg.RefreshSkew = defaultRefreshSkew
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.TokenStore == nil {
		cfg.TokenStore = NewMemoryTokenStore()
	}
	if cfg.DeviceStore == nil {
		cfg.DeviceStore = NewMemoryDeviceStore()
	}
	if cfg.BrowserOpener == nil {
		cfg.BrowserOpener = SystemBrowserOpener{}
	}

	ep := endpoints{
		baseURL:               cfg.BaseURL,
		apiBaseURL:            cfg.APIBaseURL,
		authorizationEndpoint: cfg.AuthorizationEndpoint,
		tokenEndpoint:         cfg.TokenEndpoint,
		revocationEndpoint:    cfg.RevocationEndpoint,
	}
	if ep.apiBaseURL == "" {
		ep.apiBaseURL = joinURL(cfg.BaseURL, "/api/v1")
	}
	if ep.authorizationEndpoint == "" {
		ep.authorizationEndpoint = joinURL(cfg.BaseURL, "/oauth2/auth")
	}
	if ep.tokenEndpoint == "" {
		ep.tokenEndpoint = joinURL(cfg.BaseURL, "/oauth2/token")
	}
	if ep.revocationEndpoint == "" {
		ep.revocationEndpoint = joinURL(cfg.BaseURL, "/oauth2/revoke")
	}

	for _, raw := range []string{ep.apiBaseURL, ep.authorizationEndpoint, ep.tokenEndpoint, ep.revocationEndpoint} {
		if _, err := parseAbsoluteURL(raw); err != nil {
			return Config{}, endpoints{}, errors.New("umbra: derived endpoints must be absolute URLs")
		}
	}

	return cfg, ep, nil
}

func parseAbsoluteURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid url")
	}
	return parsed, nil
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
