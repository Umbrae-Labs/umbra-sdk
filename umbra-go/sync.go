package umbra

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

const SyncProtocolVersion uint32 = 1

type SyncClient struct {
	api *apiClient
}

type SyncSpace struct {
	Name string `json:"name"`
}

type SyncRecordKey struct {
	Namespace  string `json:"namespace"`
	Collection string `json:"collection"`
	RecordID   string `json:"record_id"`
}

type SyncOperation string

const (
	SyncOperationUpsert SyncOperation = "upsert"
	SyncOperationDelete SyncOperation = "delete"
)

type SyncMutation struct {
	MutationID    string          `json:"mutation_id"`
	Key           SyncRecordKey   `json:"key"`
	SchemaVersion uint32          `json:"schema_version"`
	BaseVersion   uint64          `json:"base_version"`
	Operation     SyncOperation   `json:"operation"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

type SyncExchangeInput struct {
	ProtocolVersion uint32         `json:"protocol_version"`
	Space           SyncSpace      `json:"space"`
	Cursor          string         `json:"cursor,omitempty"`
	Mutations       []SyncMutation `json:"mutations"`
	PullLimit       int            `json:"pull_limit,omitempty"`
}

type SyncAcceptedMutation struct {
	MutationID    string `json:"mutation_id"`
	RecordVersion uint64 `json:"record_version"`
	Cursor        string `json:"cursor"`
}

type SyncChange struct {
	Cursor         string          `json:"cursor"`
	Key            SyncRecordKey   `json:"key"`
	SchemaVersion  uint32          `json:"schema_version"`
	RecordVersion  uint64          `json:"record_version"`
	Operation      SyncOperation   `json:"operation"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	WriterDeviceID string          `json:"writer_device_id"`
}

func (c SyncChange) DecodePayload(out any) error {
	return json.Unmarshal(c.Payload, out)
}

type SyncConflict struct {
	MutationID string      `json:"mutation_id"`
	Reason     string      `json:"reason"`
	Current    *SyncChange `json:"current,omitempty"`
}

type SyncRejectedMutation struct {
	MutationID string `json:"mutation_id"`
	Reason     string `json:"reason"`
}

type SyncExchangeResult struct {
	Accepted       []SyncAcceptedMutation `json:"accepted"`
	Conflicts      []SyncConflict         `json:"conflicts"`
	Rejected       []SyncRejectedMutation `json:"rejected"`
	Changes        []SyncChange           `json:"changes"`
	NextCursor     string                 `json:"next_cursor"`
	HasMore        bool                   `json:"has_more"`
	ResetRequired  bool                   `json:"reset_required"`
	Reason         string                 `json:"reason,omitempty"`
	SnapshotCursor string                 `json:"snapshot_cursor,omitempty"`
}

type SyncSnapshotInput struct {
	ProtocolVersion uint32
	SpaceName       string
	Cursor          string
	Limit           int
}

type SyncSnapshotPage struct {
	Records        []SyncChange `json:"records"`
	NextCursor     string       `json:"next_cursor,omitempty"`
	ExchangeCursor string       `json:"exchange_cursor"`
	HasMore        bool         `json:"has_more"`
}

func NewUpsertMutation(mutationID string, key SyncRecordKey, schemaVersion uint32, baseVersion uint64, payload any) (SyncMutation, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return SyncMutation{}, err
	}
	return SyncMutation{
		MutationID: mutationID, Key: key, SchemaVersion: schemaVersion, BaseVersion: baseVersion,
		Operation: SyncOperationUpsert, Payload: encoded,
	}, nil
}

func NewDeleteMutation(mutationID string, key SyncRecordKey, schemaVersion uint32, baseVersion uint64) SyncMutation {
	return SyncMutation{
		MutationID: mutationID, Key: key, SchemaVersion: schemaVersion, BaseVersion: baseVersion,
		Operation: SyncOperationDelete,
	}
}

func (s *SyncClient) Exchange(ctx context.Context, input SyncExchangeInput) (*SyncExchangeResult, error) {
	if input.ProtocolVersion == 0 {
		input.ProtocolVersion = SyncProtocolVersion
	}
	if input.Mutations == nil {
		input.Mutations = []SyncMutation{}
	}
	var out SyncExchangeResult
	if err := s.api.post(ctx, "/client/sync/exchange", input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *SyncClient) Snapshot(ctx context.Context, input SyncSnapshotInput) (*SyncSnapshotPage, error) {
	if input.ProtocolVersion == 0 {
		input.ProtocolVersion = SyncProtocolVersion
	}
	query := url.Values{}
	query.Set("protocol_version", strconv.FormatUint(uint64(input.ProtocolVersion), 10))
	query.Set("space", input.SpaceName)
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	var out SyncSnapshotPage
	if err := s.api.get(ctx, "/client/sync/snapshot", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
