package umbra

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestLoopbackCallbackReturnsNoContent(t *testing.T) {
	receiver, err := NewLoopbackCallbackReceiver("http://127.0.0.1:0/auth/callback")
	if err != nil {
		t.Fatalf("create callback receiver: %v", err)
	}
	t.Cleanup(func() { _ = receiver.Close(context.Background()) })

	response, err := http.Get(receiver.RedirectURI() + "?code=test-code&state=test-state")
	if err != nil {
		t.Fatalf("request callback: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read callback response: %v", err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("callback status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if len(body) != 0 {
		t.Fatalf("callback response body = %q, want empty", body)
	}

	callback, err := receiver.Receive(context.Background(), "test-state")
	if err != nil {
		t.Fatalf("receive callback: %v", err)
	}
	if callback.Code != "test-code" {
		t.Fatalf("callback code = %q, want test-code", callback.Code)
	}
}
