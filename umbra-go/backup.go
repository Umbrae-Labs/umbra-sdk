package umbra

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
)

type BackupClient struct {
	api  *apiClient
	http *http.Client
}

type presignUploadRequest struct {
	Category    string `json:"category"`
	Subject     string `json:"subject"`
	Version     string `json:"version"`
	FileSize    uint64 `json:"file_size"`
	ContentType string `json:"content_type"`
	ContentHash string `json:"content_hash,omitempty"`
}

type presignBatchRequest struct {
	Items []presignUploadRequest `json:"items"`
}

type presignBatchResponse struct {
	Items []BatchPresignResultItem `json:"items"`
	Total int                      `json:"total"`
}

type backupTargetRequest struct {
	BackupID uint64 `json:"backup_id,omitempty"`
	Category string `json:"category,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Version  string `json:"version,omitempty"`
}

type confirmBatchRequest struct {
	Items []backupTargetRequest `json:"items"`
}

type backupListResponse struct {
	Files []BackupRecord `json:"files"`
	Total int            `json:"total"`
}

type negotiateRequest struct {
	Items []NegotiateItem `json:"items"`
}

type negotiateResponse struct {
	Items []NegotiateResult `json:"items"`
	Total int               `json:"total"`
}

func (b *BackupClient) PresignUpload(ctx context.Context, input PresignUploadInput) (*PresignUploadResult, error) {
	req, err := makePresignRequest(input)
	if err != nil {
		return nil, err
	}
	var out PresignUploadResult
	if err := b.api.post(ctx, "/client/backup/presign", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *BackupClient) PresignUploadBatch(ctx context.Context, items []PresignUploadInput) ([]BatchPresignResultItem, error) {
	reqItems := make([]presignUploadRequest, 0, len(items))
	for _, item := range items {
		req, err := makePresignRequest(item)
		if err != nil {
			return nil, err
		}
		reqItems = append(reqItems, req)
	}
	var out presignBatchResponse
	if err := b.api.post(ctx, "/client/backup/presign-batch", presignBatchRequest{Items: reqItems}, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (b *BackupClient) ConfirmUpload(ctx context.Context, target BackupTarget) (*ConfirmUploadResult, error) {
	req, err := makeTargetRequest(target)
	if err != nil {
		return nil, err
	}
	var out ConfirmUploadResult
	if err := b.api.post(ctx, "/client/backup/confirm", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *BackupClient) ConfirmUploadBatch(ctx context.Context, targets []BackupTarget) (*BatchConfirmResult, error) {
	items := make([]backupTargetRequest, 0, len(targets))
	for _, target := range targets {
		req, err := makeTargetRequest(target)
		if err != nil {
			return nil, err
		}
		items = append(items, req)
	}
	var out BatchConfirmResult
	if err := b.api.post(ctx, "/client/backup/confirm-batch", confirmBatchRequest{Items: items}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *BackupClient) PresignDownload(ctx context.Context, target BackupTarget) (*PresignDownloadResult, error) {
	req, err := makeTargetRequest(target)
	if err != nil {
		return nil, err
	}
	var out PresignDownloadResult
	if err := b.api.post(ctx, "/client/backup/presign-download", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *BackupClient) List(ctx context.Context, filter BackupListFilter) ([]BackupRecord, error) {
	q := queryNonEmpty(map[string]string{
		"category": string(filter.Category),
		"subject":  filter.Subject,
	})
	var out backupListResponse
	if err := b.api.get(ctx, "/client/backup/list", q, &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

func (b *BackupClient) Negotiate(ctx context.Context, items []NegotiateItem) ([]NegotiateResult, error) {
	for i := range items {
		hash, err := normalizeContentHash(items[i].ContentHash, false)
		if err != nil {
			return nil, err
		}
		items[i].ContentHash = hash
	}
	var out negotiateResponse
	if err := b.api.post(ctx, "/client/backup/negotiate", negotiateRequest{Items: items}, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (b *BackupClient) Delete(ctx context.Context, target BackupTarget) (*DeleteResult, error) {
	req, err := makeTargetRequest(target)
	if err != nil {
		return nil, err
	}
	var out DeleteResult
	if err := b.api.delete(ctx, "/client/backup/file", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *BackupClient) UploadFile(ctx context.Context, address BackupAddress, path string, options UploadOptions) (*UploadResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < 0 {
		return nil, invalidInput("file size is invalid")
	}
	if options.ContentType == "" {
		options.ContentType = contentTypeForPath(path)
	}
	if options.ComputeHash || options.NegotiateByHash {
		hash, err := hashFile(file)
		if err != nil {
			return nil, err
		}
		options.ContentHash = hash
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return b.UploadReader(ctx, address, file, uint64(info.Size()), options)
}

func (b *BackupClient) UploadReader(ctx context.Context, address BackupAddress, reader io.Reader, size uint64, options UploadOptions) (*UploadResult, error) {
	if options.ContentType == "" {
		options.ContentType = "application/octet-stream"
	}
	contentHash, err := normalizeContentHash(options.ContentHash, true)
	if err != nil {
		return nil, err
	}
	if options.NegotiateByHash && contentHash != "" {
		results, err := b.Negotiate(ctx, []NegotiateItem{{
			Category:    address.Category,
			Subject:     address.Subject,
			ContentHash: contentHash,
		}})
		if err != nil {
			return nil, err
		}
		if len(results) > 0 && results[0].Exists {
			return &UploadResult{
				BackupID:  results[0].BackupID,
				SizeBytes: results[0].SizeBytes,
				Skipped:   true,
			}, nil
		}
	}

	presign, err := b.PresignUpload(ctx, PresignUploadInput{
		Address:     address,
		FileSize:    size,
		ContentType: options.ContentType,
		ContentHash: contentHash,
	})
	if err != nil {
		return nil, err
	}

	body := io.Reader(reader)
	if options.Progress != nil {
		body = &progressReader{reader: reader, total: size, progress: options.Progress}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, presign.PresignedURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", options.ContentType)
	req.ContentLength = int64(size)

	res, err := b.http.Do(req)
	if err != nil {
		return nil, wrapNetwork(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &UmbraError{Kind: ErrStorageUnavailable, HTTPStatus: res.StatusCode, Message: "object storage upload failed"}
	}

	confirmed, err := b.ConfirmUpload(ctx, BackupTarget{BackupID: presign.BackupID})
	if err != nil {
		return nil, err
	}
	return &UploadResult{
		BackupID:  confirmed.BackupID,
		SizeBytes: confirmed.SizeBytes,
		ETag:      confirmed.ETag,
		Quota:     confirmed.Quota,
	}, nil
}

func (b *BackupClient) DownloadFile(ctx context.Context, target BackupTarget, path string, options DownloadOptions) (*DownloadResult, error) {
	if !options.Overwrite {
		if _, err := os.Stat(path); err == nil {
			return nil, invalidInput("target file already exists")
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	result, copyErr := b.DownloadWriter(ctx, target, file, options)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return nil, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return nil, closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return result, nil
}

func (b *BackupClient) DownloadWriter(ctx context.Context, target BackupTarget, writer io.Writer, options DownloadOptions) (*DownloadResult, error) {
	presign, err := b.PresignDownload(ctx, target)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, presign.PresignedURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := b.http.Do(req)
	if err != nil {
		return nil, wrapNetwork(err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, &UmbraError{Kind: ErrStorageUnavailable, HTTPStatus: res.StatusCode, Message: "object storage download failed"}
	}
	src := io.Reader(res.Body)
	if options.Progress != nil {
		src = &progressReader{reader: res.Body, total: presign.SizeBytes, progress: options.Progress}
	}
	if _, err := io.Copy(writer, src); err != nil {
		return nil, err
	}
	return &DownloadResult{
		BackupID:  presign.BackupID,
		SizeBytes: presign.SizeBytes,
		ETag:      presign.ETag,
	}, nil
}

func makePresignRequest(input PresignUploadInput) (presignUploadRequest, error) {
	if err := ValidateAddress(input.Address); err != nil {
		return presignUploadRequest{}, err
	}
	if input.FileSize == 0 {
		return presignUploadRequest{}, invalidInput("file size must be greater than zero")
	}
	if input.ContentType == "" {
		return presignUploadRequest{}, invalidInput("content type is required")
	}
	hash, err := normalizeContentHash(input.ContentHash, true)
	if err != nil {
		return presignUploadRequest{}, err
	}
	return presignUploadRequest{
		Category:    string(input.Address.Category),
		Subject:     input.Address.Subject,
		Version:     input.Address.Version,
		FileSize:    input.FileSize,
		ContentType: input.ContentType,
		ContentHash: hash,
	}, nil
}

func makeTargetRequest(target BackupTarget) (backupTargetRequest, error) {
	if target.BackupID > 0 {
		return backupTargetRequest{BackupID: target.BackupID}, nil
	}
	if err := ValidateAddress(target.Address); err != nil {
		return backupTargetRequest{}, err
	}
	return backupTargetRequest{
		Category: string(target.Address.Category),
		Subject:  target.Address.Subject,
		Version:  target.Address.Version,
	}, nil
}

func contentTypeForPath(path string) string {
	if ext := filepath.Ext(path); ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			return ct
		}
	}
	return "application/octet-stream"
}

func hashFile(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressReader struct {
	reader   io.Reader
	done     uint64
	total    uint64
	progress func(done, total uint64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.done += uint64(n)
		r.progress(r.done, r.total)
	}
	return n, err
}
