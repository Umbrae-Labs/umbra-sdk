package umbra

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

type CallbackReceiver interface {
	Receive(ctx context.Context, expectedState string) (*AuthCallback, error)
}

// LoopbackCallbackReceiver receives OAuth redirects on 127.0.0.1.
type LoopbackCallbackReceiver struct {
	redirectURI string
	server      *http.Server
	resultCh    chan *AuthCallback
	errCh       chan error
}

func NewLoopbackCallbackReceiver(redirectURI string) (*LoopbackCallbackReceiver, error) {
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
		return nil, invalidInput("redirect URI must be http://127.0.0.1:<port>/<path>")
	}
	port := parsed.Port()
	if port == "" {
		port = "0"
	}
	path := parsed.Path
	if path == "" {
		path = "/auth/callback"
	}

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, err
	}
	actual := listener.Addr().(*net.TCPAddr)
	parsed.Host = net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", actual.Port))
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""

	receiver := &LoopbackCallbackReceiver{
		redirectURI: parsed.String(),
		resultCh:    make(chan *AuthCallback, 1),
		errCh:       make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		cb := &AuthCallback{
			Code:  r.URL.Query().Get("code"),
			State: r.URL.Query().Get("state"),
			Error: r.URL.Query().Get("error"),
		}
		if cb.Error == "" && cb.Code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Umbra</title><p>Authorization completed. You can close this window.</p>"))
		select {
		case receiver.resultCh <- cb:
		default:
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	receiver.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := receiver.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			receiver.errCh <- err
		}
	}()
	return receiver, nil
}

func (r *LoopbackCallbackReceiver) RedirectURI() string {
	return r.redirectURI
}

func (r *LoopbackCallbackReceiver) Receive(ctx context.Context, expectedState string) (*AuthCallback, error) {
	select {
	case cb := <-r.resultCh:
		if expectedState != "" && cb.State != expectedState {
			return nil, authError("authorization state mismatch")
		}
		return cb, nil
	case err := <-r.errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *LoopbackCallbackReceiver) Close(ctx context.Context) error {
	if r == nil || r.server == nil {
		return nil
	}
	return r.server.Shutdown(ctx)
}
