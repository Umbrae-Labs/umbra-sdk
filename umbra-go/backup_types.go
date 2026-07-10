package umbra

import (
	"regexp"
	"strings"
	"time"
)

type BackupCategory string

const (
	CategoryDB    BackupCategory = "db"
	CategoryFull  BackupCategory = "full"
	CategoryGame  BackupCategory = "game"
	CategoryAsset BackupCategory = "asset"
)

type BackupAddress struct {
	Category BackupCategory `json:"category,omitempty"`
	Subject  string         `json:"subject,omitempty"`
	Version  string         `json:"version,omitempty"`
}

func DBBackup(version string) BackupAddress {
	return BackupAddress{Category: CategoryDB, Version: version}
}

func FullBackup(version string) BackupAddress {
	return BackupAddress{Category: CategoryFull, Version: version}
}

func GameBackup(subject, version string) BackupAddress {
	return BackupAddress{Category: CategoryGame, Subject: subject, Version: version}
}

func AssetBackup(subject, version string) BackupAddress {
	return BackupAddress{Category: CategoryAsset, Subject: subject, Version: version}
}

var (
	subjectPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	versionPattern     = regexp.MustCompile(`^[A-Za-z0-9_\-.:]{1,64}$`)
	contentHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func ValidateAddress(address BackupAddress) error {
	category := BackupCategory(strings.TrimSpace(string(address.Category)))
	subject := strings.TrimSpace(address.Subject)
	version := strings.TrimSpace(address.Version)
	switch category {
	case CategoryDB, CategoryFull:
		if subject != "" {
			return invalidInput("subject must be empty for %s backups", category)
		}
	case CategoryGame, CategoryAsset:
		if !subjectPattern.MatchString(subject) {
			return invalidInput("subject must match %s", subjectPattern.String())
		}
	default:
		return invalidInput("invalid backup category")
	}
	if !versionPattern.MatchString(version) {
		return invalidInput("version must match %s", versionPattern.String())
	}
	return nil
}

func normalizeContentHash(hash string, allowEmpty bool) (string, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		if allowEmpty {
			return "", nil
		}
		return "", invalidInput("content hash is required")
	}
	if !contentHashPattern.MatchString(hash) {
		return "", invalidInput("content hash must be lowercase SHA-256 hex")
	}
	return hash, nil
}

type PresignUploadInput struct {
	Address     BackupAddress
	FileSize    uint64
	ContentType string
	ContentHash string
}

type BackupTarget struct {
	BackupID uint64
	Address  BackupAddress
}

type BackupListFilter struct {
	Category BackupCategory
	Subject  string
}

type PresignUploadResult struct {
	BackupID     uint64 `json:"backup_id"`
	PresignedURL string `json:"presigned_url"`
	ExpiresIn    int64  `json:"expires_in"`
}

type PresignDownloadResult struct {
	BackupID     uint64 `json:"backup_id"`
	PresignedURL string `json:"presigned_url"`
	ExpiresIn    int64  `json:"expires_in"`
	SizeBytes    uint64 `json:"size_bytes"`
	ETag         string `json:"etag"`
}

type ConfirmUploadResult struct {
	Quota     QuotaInfo `json:"quota"`
	BackupID  uint64    `json:"backup_id"`
	SizeBytes uint64    `json:"size_bytes"`
	ETag      string    `json:"etag"`
}

type BatchItemError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type BatchPresignResultItem struct {
	BackupID     uint64          `json:"backup_id,omitempty"`
	PresignedURL string          `json:"presigned_url,omitempty"`
	ExpiresIn    int64           `json:"expires_in,omitempty"`
	Error        *BatchItemError `json:"error,omitempty"`
}

type BatchConfirmResultItem struct {
	BackupID  uint64          `json:"backup_id,omitempty"`
	SizeBytes uint64          `json:"size_bytes,omitempty"`
	ETag      string          `json:"etag,omitempty"`
	Error     *BatchItemError `json:"error,omitempty"`
}

type BatchConfirmResult struct {
	Items []BatchConfirmResultItem `json:"items"`
	Total int                      `json:"total"`
	Quota QuotaInfo                `json:"quota"`
}

type BackupRecord struct {
	BackupID    uint64    `json:"backup_id"`
	Category    string    `json:"category"`
	Subject     string    `json:"subject"`
	Version     string    `json:"version"`
	SizeBytes   uint64    `json:"size_bytes"`
	ContentHash string    `json:"content_hash,omitempty"`
	ETag        string    `json:"etag,omitempty"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

type NegotiateItem struct {
	Category    BackupCategory `json:"category"`
	Subject     string         `json:"subject"`
	ContentHash string         `json:"content_hash"`
}

type NegotiateResult struct {
	Category    string `json:"category"`
	Subject     string `json:"subject"`
	ContentHash string `json:"content_hash"`
	Exists      bool   `json:"exists"`
	BackupID    uint64 `json:"backup_id,omitempty"`
	Version     string `json:"version,omitempty"`
	SizeBytes   uint64 `json:"size_bytes,omitempty"`
}

type DeleteResult struct {
	FreedBytes     uint64 `json:"freed_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

type UploadOptions struct {
	ContentType     string
	ContentHash     string
	ComputeHash     bool
	NegotiateByHash bool
	Progress        func(done, total uint64)
}

type UploadResult struct {
	BackupID  uint64
	SizeBytes uint64
	ETag      string
	Quota     QuotaInfo
	Skipped   bool
}

type DownloadOptions struct {
	Progress  func(done, total uint64)
	Overwrite bool
}

type DownloadResult struct {
	BackupID  uint64
	SizeBytes uint64
	ETag      string
}
