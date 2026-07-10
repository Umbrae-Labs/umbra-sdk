package umbra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSyncExchangeIsSignedAndReturnsConflictsAsData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/client/sync/exchange", func(w http.ResponseWriter, r *http.Request) {
		var input SyncExchangeInput
		raw := decodeJSONBody(t, r, &input)
		assertSignedRequest(t, r, raw, "device-1", "device-secret")
		if input.Space.Name != "library" || len(input.Mutations) != 1 {
			t.Fatalf("unexpected input: %+v", input)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "success",
			"data": SyncExchangeResult{
				Accepted: []SyncAcceptedMutation{}, Rejected: []SyncRejectedMutation{}, Changes: []SyncChange{},
				Conflicts:  []SyncConflict{{MutationID: "m-1", Reason: "base_version_mismatch"}},
				NextCursor: "cursor-1",
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newSignedTestClient(t, server.URL, "device-1", "device-secret")
	mutation, err := NewUpsertMutation("m-1", SyncRecordKey{Namespace: "lunabox.library", Collection: "games", RecordID: "game-1"}, 1, 2, map[string]any{"name": "Example"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Sync.Exchange(context.Background(), SyncExchangeInput{Space: SyncSpace{Name: "library"}, Mutations: []SyncMutation{mutation}})
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].MutationID != "m-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSyncSnapshotQueryIsSigned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/client/sync/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("protocol_version") != "1" || r.URL.Query().Get("space") != "library" || r.URL.Query().Get("cursor") != "page-1" || r.URL.Query().Get("limit") != "25" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		assertSignedRequest(t, r, nil, "device-1", "device-secret")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "success", "data": SyncSnapshotPage{Records: []SyncChange{}, ExchangeCursor: "cursor-2"},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newSignedTestClient(t, server.URL, "device-1", "device-secret")
	result, err := client.Sync.Snapshot(context.Background(), SyncSnapshotInput{SpaceName: "library", Cursor: "page-1", Limit: 25})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if result.ExchangeCursor != "cursor-2" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSyncChangeDecodePayload(t *testing.T) {
	change := SyncChange{Payload: json.RawMessage(`{"name":"Example"}`)}
	var payload struct {
		Name string `json:"name"`
	}
	if err := change.DecodePayload(&payload); err != nil || payload.Name != "Example" {
		t.Fatalf("DecodePayload() = %+v, %v", payload, err)
	}
}
