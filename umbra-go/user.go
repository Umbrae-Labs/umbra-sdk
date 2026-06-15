package umbra

import "context"

type UserClient struct {
	api *apiClient
}

type QuotaInfo struct {
	QuotaBytes     uint64 `json:"quota_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

type UserProfile struct {
	ID             uint64  `json:"id"`
	Username       string  `json:"username"`
	QuotaBytes     uint64  `json:"quota_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	StorageEndID   *uint64 `json:"storage_end_id,omitempty"`
}

func (u *UserClient) Quota(ctx context.Context) (*QuotaInfo, error) {
	var out QuotaInfo
	if err := u.api.get(ctx, "/user/quota", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (u *UserClient) Profile(ctx context.Context) (*UserProfile, error) {
	var out UserProfile
	if err := u.api.get(ctx, "/user/profile", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
