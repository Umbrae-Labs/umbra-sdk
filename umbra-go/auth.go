package umbra

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AuthClient handles OAuth Authorization Code + PKCE.
type AuthClient struct {
	config    Config
	endpoints endpoints
	http      *http.Client
	store     TokenStore
	opener    BrowserOpener
	callback  CallbackReceiver

	mu sync.Mutex
}

type Session struct {
	Token *TokenSet
}

type TokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type AuthCallback struct {
	Code  string
	State string
	Error string
}

type hydraTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

// Login opens the browser, waits for the redirect callback, exchanges the code,
// and stores the resulting token set.
func (a *AuthClient) Login(ctx context.Context) (*Session, error) {
	verifier, challenge, err := newPKCE()
	if err != nil {
		return nil, err
	}
	state, err := randHex(16)
	if err != nil {
		return nil, err
	}

	receiver := a.callback
	redirectURI := a.config.RedirectURI
	var loopback *LoopbackCallbackReceiver
	if receiver == nil {
		loopback, err = NewLoopbackCallbackReceiver(redirectURI)
		if err != nil {
			return nil, err
		}
		defer loopback.Close(context.Background())
		receiver = loopback
		redirectURI = loopback.RedirectURI()
	}

	authorizeURL := a.buildAuthorizeURL(redirectURI, state, challenge)
	if err := a.opener.OpenURL(ctx, authorizeURL); err != nil {
		return nil, err
	}

	cb, err := receiver.Receive(ctx, state)
	if err != nil {
		return nil, err
	}
	if cb.Error != "" {
		return nil, authError("authorization failed: %s", cb.Error)
	}
	if cb.Code == "" {
		return nil, authError("authorization callback missing code")
	}
	if cb.State != state {
		return nil, authError("authorization state mismatch")
	}

	token, err := a.exchangeCode(ctx, cb.Code, verifier, redirectURI)
	if err != nil {
		return nil, err
	}
	if err := a.store.Save(ctx, token); err != nil {
		return nil, err
	}
	return &Session{Token: token}, nil
}

func (a *AuthClient) buildAuthorizeURL(redirectURI, state, challenge string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", a.config.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", a.config.Scope)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	return a.endpoints.authorizationEndpoint + "?" + q.Encode()
}

func (a *AuthClient) exchangeCode(ctx context.Context, code, verifier, redirectURI string) (*TokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", a.config.ClientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	return a.tokenRequest(ctx, form)
}

// Refresh refreshes the current token set with refresh_token.
func (a *AuthClient) Refresh(ctx context.Context) (*TokenSet, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	current, err := a.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if current == nil || current.RefreshToken == "" {
		return nil, authError("refresh token is not available")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", a.config.ClientID)
	form.Set("refresh_token", current.RefreshToken)

	token, err := a.tokenRequest(ctx, form)
	if err != nil {
		return nil, err
	}
	if token.RefreshToken == "" {
		token.RefreshToken = current.RefreshToken
	}
	if err := a.store.Save(ctx, token); err != nil {
		return nil, err
	}
	return token, nil
}

// Logout revokes the current tokens where possible and clears local storage.
func (a *AuthClient) Logout(ctx context.Context) error {
	token, err := a.store.Load(ctx)
	if err != nil {
		return err
	}
	if token != nil {
		if token.RefreshToken != "" {
			_ = a.revoke(ctx, token.RefreshToken, "refresh_token")
		}
		if token.AccessToken != "" {
			_ = a.revoke(ctx, token.AccessToken, "access_token")
		}
	}
	return a.store.Clear(ctx)
}

// Token returns a valid access token, refreshing it if needed.
func (a *AuthClient) Token(ctx context.Context) (*TokenSet, error) {
	token, err := a.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if token == nil || token.AccessToken == "" {
		return nil, authError("not authenticated")
	}
	if token.ExpiresAt.IsZero() || time.Until(token.ExpiresAt) > a.config.RefreshSkew {
		return token, nil
	}
	if token.RefreshToken == "" {
		return token, nil
	}
	return a.Refresh(ctx)
}

func (a *AuthClient) IsAuthenticated(ctx context.Context) bool {
	token, err := a.Token(ctx)
	return err == nil && token != nil && token.AccessToken != ""
}

func (a *AuthClient) tokenRequest(ctx context.Context, form url.Values) (*TokenSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoints.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := a.http.Do(req)
	if err != nil {
		return nil, wrapNetwork(err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &UmbraError{
			Kind:       ErrAuth,
			HTTPStatus: res.StatusCode,
			Message:    fmt.Sprintf("token endpoint returned %d: %s", res.StatusCode, truncate(string(body), 300)),
		}
	}

	var out hydraTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, authError("token response missing access_token")
	}
	tokenType := out.TokenType
	if tokenType == "" {
		tokenType = "bearer"
	}
	expiresAt := time.Time{}
	if out.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	return &TokenSet{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		TokenType:    tokenType,
		Scope:        out.Scope,
		ExpiresAt:    expiresAt,
	}, nil
}

func (a *AuthClient) revoke(ctx context.Context, token string, tokenTypeHint string) error {
	form := url.Values{}
	form.Set("token", token)
	if tokenTypeHint != "" {
		form.Set("token_type_hint", tokenTypeHint)
	}
	form.Set("client_id", a.config.ClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoints.revocationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := a.http.Do(req)
	if err != nil {
		return wrapNetwork(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &UmbraError{Kind: ErrAuth, HTTPStatus: res.StatusCode, Message: "token revocation failed"}
	}
	return nil
}

func newPKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 48)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func errorsIsContext(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
