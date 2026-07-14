package umbra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type apiClient struct {
	http        *http.Client
	baseURL     string
	auth        *AuthClient
	deviceStore DeviceStore
	userAgent   string
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (c *apiClient) get(ctx context.Context, path string, query url.Values, out any) error {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return c.doJSON(ctx, http.MethodGet, path, nil, out, true)
}

func (c *apiClient) post(ctx context.Context, path string, in any, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, in, out, true)
}

func (c *apiClient) delete(ctx context.Context, path string, in any, out any) error {
	return c.doJSON(ctx, http.MethodDelete, path, in, out, true)
}

func (c *apiClient) doJSON(ctx context.Context, method string, path string, in any, out any, retryAuth bool) error {
	var signer *requestSigner
	if isDeviceSignedPath(path) {
		credentials, err := c.loadDeviceCredentials(ctx)
		if err != nil {
			return err
		}
		signer = &requestSigner{Secret: credentials.DeviceSecret, DeviceID: credentials.DeviceID}
	}
	return c.doJSONWithSigner(ctx, method, path, in, out, retryAuth, signer)
}

func (c *apiClient) postSigned(ctx context.Context, path string, in any, out any, signer requestSigner) error {
	return c.doJSONWithSigner(ctx, http.MethodPost, path, in, out, true, &signer)
}

func (c *apiClient) doJSONWithSigner(ctx context.Context, method string, path string, in any, out any, retryAuth bool, signer *requestSigner) error {
	body, err := encodeBody(in)
	if err != nil {
		return err
	}
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, joinURL(c.baseURL, path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.auth != nil {
		token, err := c.auth.Token(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}
	if signer != nil {
		if err := signRequest(req, body, *signer); err != nil {
			return err
		}
	}

	res, err := c.http.Do(req)
	if err != nil {
		if errorsIsContext(err) {
			return err
		}
		return wrapNetwork(err)
	}
	defer res.Body.Close()

	err = decodeEnvelope(res, out)
	if retryAuth && isInvalidToken(err) && c.auth != nil {
		if _, refreshErr := c.auth.Refresh(ctx); refreshErr != nil {
			return err
		}
		return c.doJSONWithSigner(ctx, method, path, in, out, false, signer)
	}
	return err
}

func (c *apiClient) loadDeviceCredentials(ctx context.Context) (*DeviceCredentials, error) {
	if c.deviceStore == nil {
		return nil, invalidInput("device credentials are not configured")
	}
	credentials, err := c.deviceStore.Load(ctx)
	if err != nil {
		return nil, err
	}
	if credentials == nil || credentials.DeviceID == "" || credentials.DeviceSecret == "" {
		return nil, invalidInput("device credentials are not available")
	}
	return credentials, nil
}

func encodeBody(in any) ([]byte, error) {
	if in == nil {
		return nil, nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func isDeviceSignedPath(path string) bool {
	path = strings.TrimSpace(path)
	return path == "/client/backup" || strings.HasPrefix(path, "/client/backup/") || strings.HasPrefix(path, "client/backup/") ||
		path == "/client/sync" || strings.HasPrefix(path, "/client/sync/") || strings.HasPrefix(path, "client/sync/") ||
		path == "/client/devices/logout" || path == "client/devices/logout"
}

func decodeEnvelope(res *http.Response, out any) error {
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return &UmbraError{Kind: kindForCode(res.StatusCode, 0), HTTPStatus: res.StatusCode, Message: fmt.Sprintf("request failed with status %d", res.StatusCode)}
		}
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || env.Code != 0 {
		return apiError(res.StatusCode, env.Code, env.Msg)
	}
	if out == nil {
		return nil
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func queryNonEmpty(values map[string]string) url.Values {
	q := url.Values{}
	for key, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			q.Set(key, value)
		}
	}
	return q
}
