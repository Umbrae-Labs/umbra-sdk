package umbra

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// TokenStore persists OAuth tokens.
type TokenStore interface {
	Load(ctx context.Context) (*TokenSet, error)
	Save(ctx context.Context, token *TokenSet) error
	Clear(ctx context.Context) error
}

// MemoryTokenStore stores tokens in process memory.
type MemoryTokenStore struct {
	mu    sync.RWMutex
	token *TokenSet
}

func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{}
}

func (s *MemoryTokenStore) Load(context.Context) (*TokenSet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.token == nil {
		return nil, nil
	}
	copy := *s.token
	return &copy, nil
}

func (s *MemoryTokenStore) Save(_ context.Context, token *TokenSet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == nil {
		s.token = nil
		return nil
	}
	copy := *token
	s.token = &copy
	return nil
}

func (s *MemoryTokenStore) Clear(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = nil
	return nil
}

// FileTokenStore stores tokens in a JSON file. It is intended for development
// and examples; production desktop apps should prefer OS keychain storage.
type FileTokenStore struct {
	path string
	mu   sync.Mutex
}

func NewFileTokenStore(path string) *FileTokenStore {
	return &FileTokenStore{path: path}
}

func (s *FileTokenStore) Load(ctx context.Context) (*TokenSet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var token TokenSet
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func (s *FileTokenStore) Save(ctx context.Context, token *TokenSet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil && filepath.Dir(s.path) != "." {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *FileTokenStore) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// DeviceStore persists the device credentials used for signed client requests.
type DeviceStore interface {
	Load(ctx context.Context) (*DeviceCredentials, error)
	Save(ctx context.Context, credentials *DeviceCredentials) error
	Clear(ctx context.Context) error
}

// MemoryDeviceStore stores device credentials in process memory.
type MemoryDeviceStore struct {
	mu          sync.RWMutex
	credentials *DeviceCredentials
}

func NewMemoryDeviceStore() *MemoryDeviceStore {
	return &MemoryDeviceStore{}
}

func (s *MemoryDeviceStore) Load(context.Context) (*DeviceCredentials, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.credentials == nil {
		return nil, nil
	}
	copy := *s.credentials
	return &copy, nil
}

func (s *MemoryDeviceStore) Save(_ context.Context, credentials *DeviceCredentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if credentials == nil {
		s.credentials = nil
		return nil
	}
	copy := *credentials
	s.credentials = &copy
	return nil
}

func (s *MemoryDeviceStore) Clear(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentials = nil
	return nil
}

// FileDeviceStore stores device credentials in a JSON file. Production desktop
// apps should prefer OS keychain storage.
type FileDeviceStore struct {
	path string
	mu   sync.Mutex
}

func NewFileDeviceStore(path string) *FileDeviceStore {
	return &FileDeviceStore{path: path}
}

func (s *FileDeviceStore) Load(ctx context.Context) (*DeviceCredentials, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var credentials DeviceCredentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, err
	}
	return &credentials, nil
}

func (s *FileDeviceStore) Save(ctx context.Context, credentials *DeviceCredentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil && filepath.Dir(s.path) != "." {
		return err
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *FileDeviceStore) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
