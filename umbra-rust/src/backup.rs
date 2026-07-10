use std::{path::Path, pin::Pin, sync::Arc};

use chrono::{DateTime, Utc};
use regex::Regex;
use reqwest::Body;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tokio::{
    fs::File,
    io::{AsyncRead, AsyncWrite, AsyncWriteExt},
};
use tokio_util::io::ReaderStream;

use crate::{api::ApiClient, QuotaInfo, UmbraError};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum BackupCategory {
    Db,
    Full,
    Game,
    Asset,
}

impl BackupCategory {
    fn as_str(self) -> &'static str {
        match self {
            BackupCategory::Db => "db",
            BackupCategory::Full => "full",
            BackupCategory::Game => "game",
            BackupCategory::Asset => "asset",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BackupAddress {
    pub category: BackupCategory,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub subject: Option<String>,
    pub version: String,
}

impl BackupAddress {
    pub fn db(version: impl Into<String>) -> Self {
        Self {
            category: BackupCategory::Db,
            subject: None,
            version: version.into(),
        }
    }

    pub fn full(version: impl Into<String>) -> Self {
        Self {
            category: BackupCategory::Full,
            subject: None,
            version: version.into(),
        }
    }

    pub fn game(subject: impl Into<String>, version: impl Into<String>) -> Self {
        Self {
            category: BackupCategory::Game,
            subject: Some(subject.into()),
            version: version.into(),
        }
    }

    pub fn asset(subject: impl Into<String>, version: impl Into<String>) -> Self {
        Self {
            category: BackupCategory::Asset,
            subject: Some(subject.into()),
            version: version.into(),
        }
    }

    pub fn validate(&self) -> Result<(), UmbraError> {
        let subject_re = Regex::new(r"^[A-Za-z0-9_-]{1,64}$").expect("valid regex");
        let version_re = Regex::new(r"^[A-Za-z0-9_\-.:]{1,64}$").expect("valid regex");
        match self.category {
            BackupCategory::Db | BackupCategory::Full => {
                if self.subject.as_deref().unwrap_or("").trim() != "" {
                    return Err(UmbraError::invalid_input(
                        "subject must be empty for db/full backups",
                    ));
                }
            }
            BackupCategory::Game | BackupCategory::Asset => {
                let subject = self.subject.as_deref().unwrap_or("");
                if !subject_re.is_match(subject) {
                    return Err(UmbraError::invalid_input(
                        "subject must match ^[A-Za-z0-9_-]{1,64}$",
                    ));
                }
            }
        }
        if !version_re.is_match(&self.version) {
            return Err(UmbraError::invalid_input(
                "version must match ^[A-Za-z0-9_\\-.:]{1,64}$",
            ));
        }
        Ok(())
    }
}

#[derive(Clone)]
pub struct BackupClient {
    api: Arc<ApiClient>,
    http: reqwest::Client,
}

#[derive(Debug, Clone)]
pub struct PresignUploadInput {
    pub address: BackupAddress,
    pub file_size: u64,
    pub content_type: String,
    pub content_hash: Option<String>,
}

#[derive(Debug, Clone)]
pub struct BackupTarget {
    pub backup_id: Option<u64>,
    pub address: Option<BackupAddress>,
}

impl BackupTarget {
    pub fn id(backup_id: u64) -> Self {
        Self {
            backup_id: Some(backup_id),
            address: None,
        }
    }

    pub fn address(address: BackupAddress) -> Self {
        Self {
            backup_id: None,
            address: Some(address),
        }
    }
}

#[derive(Debug, Clone, Default)]
pub struct BackupListFilter {
    pub category: Option<BackupCategory>,
    pub subject: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PresignUploadResult {
    pub backup_id: u64,
    pub presigned_url: String,
    pub expires_in: i64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PresignDownloadResult {
    pub backup_id: u64,
    pub presigned_url: String,
    pub expires_in: i64,
    pub size_bytes: u64,
    pub etag: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ConfirmUploadResult {
    pub quota: QuotaInfo,
    pub backup_id: u64,
    pub size_bytes: u64,
    pub etag: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BatchItemError {
    pub code: String,
    pub message: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BatchPresignResultItem {
    #[serde(default)]
    pub backup_id: Option<u64>,
    #[serde(default)]
    pub presigned_url: Option<String>,
    #[serde(default)]
    pub expires_in: Option<i64>,
    #[serde(default)]
    pub error: Option<BatchItemError>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BatchConfirmResultItem {
    #[serde(default)]
    pub backup_id: Option<u64>,
    #[serde(default)]
    pub size_bytes: Option<u64>,
    #[serde(default)]
    pub etag: Option<String>,
    #[serde(default)]
    pub error: Option<BatchItemError>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BatchConfirmResult {
    pub items: Vec<BatchConfirmResultItem>,
    pub total: usize,
    pub quota: QuotaInfo,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BackupRecord {
    pub backup_id: u64,
    pub category: String,
    pub subject: String,
    pub version: String,
    pub size_bytes: u64,
    #[serde(default)]
    pub content_hash: Option<String>,
    #[serde(default)]
    pub etag: Option<String>,
    pub uploaded_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize)]
pub struct NegotiateItem {
    pub category: BackupCategory,
    pub subject: String,
    pub content_hash: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct NegotiateResult {
    pub category: String,
    pub subject: String,
    pub content_hash: String,
    pub exists: bool,
    #[serde(default)]
    pub backup_id: Option<u64>,
    #[serde(default)]
    pub version: Option<String>,
    #[serde(default)]
    pub size_bytes: Option<u64>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DeleteResult {
    pub freed_bytes: u64,
    pub available_bytes: u64,
}

#[derive(Default)]
pub struct UploadOptions {
    pub content_type: Option<String>,
    pub content_hash: Option<String>,
    pub compute_hash: bool,
    pub negotiate_by_hash: bool,
    pub progress: Option<Arc<dyn Fn(u64, u64) + Send + Sync>>,
}

#[derive(Debug, Clone)]
pub struct UploadResult {
    pub backup_id: u64,
    pub size_bytes: u64,
    pub etag: Option<String>,
    pub quota: Option<QuotaInfo>,
    pub skipped: bool,
}

#[derive(Default)]
pub struct DownloadOptions {
    pub progress: Option<Arc<dyn Fn(u64, u64) + Send + Sync>>,
    pub overwrite: bool,
}

#[derive(Debug, Clone)]
pub struct DownloadResult {
    pub backup_id: u64,
    pub size_bytes: u64,
    pub etag: String,
}

#[derive(Serialize)]
struct PresignUploadRequest {
    category: String,
    subject: String,
    version: String,
    file_size: u64,
    content_type: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    content_hash: Option<String>,
}

#[derive(Serialize)]
struct PresignBatchRequest {
    items: Vec<PresignUploadRequest>,
}

#[derive(Deserialize)]
struct PresignBatchResponse {
    items: Vec<BatchPresignResultItem>,
    #[allow(dead_code)]
    total: usize,
}

#[derive(Serialize)]
struct BackupTargetRequest {
    #[serde(skip_serializing_if = "Option::is_none")]
    backup_id: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    category: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    subject: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    version: Option<String>,
}

#[derive(Serialize)]
struct ConfirmBatchRequest {
    items: Vec<BackupTargetRequest>,
}

#[derive(Deserialize)]
struct BackupListResponse {
    files: Vec<BackupRecord>,
    #[allow(dead_code)]
    total: usize,
}

#[derive(Serialize)]
struct NegotiateRequest {
    items: Vec<NegotiateItem>,
}

#[derive(Deserialize)]
struct NegotiateResponse {
    items: Vec<NegotiateResult>,
    #[allow(dead_code)]
    total: usize,
}

impl BackupClient {
    pub(crate) fn new(api: Arc<ApiClient>, http: reqwest::Client) -> Self {
        Self { api, http }
    }

    pub async fn presign_upload(
        &self,
        input: PresignUploadInput,
    ) -> Result<PresignUploadResult, UmbraError> {
        let request = make_presign_request(input)?;
        self.api.post("/client/backup/presign", &request).await
    }

    pub async fn presign_upload_batch(
        &self,
        items: Vec<PresignUploadInput>,
    ) -> Result<Vec<BatchPresignResultItem>, UmbraError> {
        let mut req_items = Vec::with_capacity(items.len());
        for item in items {
            req_items.push(make_presign_request(item)?);
        }
        let out: PresignBatchResponse = self
            .api
            .post(
                "/client/backup/presign-batch",
                &PresignBatchRequest { items: req_items },
            )
            .await?;
        Ok(out.items)
    }

    pub async fn confirm_upload(
        &self,
        target: BackupTarget,
    ) -> Result<ConfirmUploadResult, UmbraError> {
        let request = make_target_request(target)?;
        self.api.post("/client/backup/confirm", &request).await
    }

    pub async fn confirm_upload_batch(
        &self,
        targets: Vec<BackupTarget>,
    ) -> Result<BatchConfirmResult, UmbraError> {
        let mut items = Vec::with_capacity(targets.len());
        for target in targets {
            items.push(make_target_request(target)?);
        }
        self.api
            .post(
                "/client/backup/confirm-batch",
                &ConfirmBatchRequest { items },
            )
            .await
    }

    pub async fn presign_download(
        &self,
        target: BackupTarget,
    ) -> Result<PresignDownloadResult, UmbraError> {
        let request = make_target_request(target)?;
        self.api
            .post("/client/backup/presign-download", &request)
            .await
    }

    pub async fn list(&self, filter: BackupListFilter) -> Result<Vec<BackupRecord>, UmbraError> {
        let mut query = Vec::new();
        if let Some(category) = filter.category {
            query.push(("category", category.as_str().to_string()));
        }
        if let Some(subject) = filter.subject {
            if !subject.is_empty() {
                query.push(("subject", subject));
            }
        }
        let out: BackupListResponse = self.api.get("/client/backup/list", &query).await?;
        Ok(out.files)
    }

    pub async fn negotiate(
        &self,
        mut items: Vec<NegotiateItem>,
    ) -> Result<Vec<NegotiateResult>, UmbraError> {
        for item in &mut items {
            item.content_hash = normalize_content_hash(Some(item.content_hash.clone()), false)?
                .expect("hash required");
        }
        let out: NegotiateResponse = self
            .api
            .post("/client/backup/negotiate", &NegotiateRequest { items })
            .await?;
        Ok(out.items)
    }

    pub async fn delete(&self, target: BackupTarget) -> Result<DeleteResult, UmbraError> {
        let request = make_target_request(target)?;
        self.api.delete("/client/backup/file", &request).await
    }

    pub async fn upload_file(
        &self,
        address: BackupAddress,
        path: impl AsRef<Path>,
        mut options: UploadOptions,
    ) -> Result<UploadResult, UmbraError> {
        let path = path.as_ref();
        let file = File::open(path).await?;
        let metadata = file.metadata().await?;
        let content_type = options.content_type.clone().unwrap_or_else(|| {
            mime_guess::from_path(path)
                .first_or_octet_stream()
                .essence_str()
                .to_owned()
        });
        options.content_type = Some(content_type);

        let reader: Pin<Box<dyn AsyncRead + Send>> =
            if options.compute_hash || options.negotiate_by_hash {
                let bytes = tokio::fs::read(path).await?;
                options.content_hash = Some(hex::encode(Sha256::digest(&bytes)));
                Box::pin(std::io::Cursor::new(bytes))
            } else {
                Box::pin(file)
            };

        self.upload_reader(address, reader, metadata.len(), options)
            .await
    }

    pub async fn upload_reader<R>(
        &self,
        address: BackupAddress,
        reader: R,
        size: u64,
        options: UploadOptions,
    ) -> Result<UploadResult, UmbraError>
    where
        R: AsyncRead + Unpin + Send + 'static,
    {
        let content_type = options
            .content_type
            .clone()
            .unwrap_or_else(|| "application/octet-stream".to_string());
        let content_hash = normalize_content_hash(options.content_hash.clone(), true)?;

        if options.negotiate_by_hash {
            if let Some(hash) = content_hash.as_deref() {
                let results = self
                    .negotiate(vec![NegotiateItem {
                        category: address.category,
                        subject: address.subject.clone().unwrap_or_default(),
                        content_hash: hash.to_string(),
                    }])
                    .await?;
                if let Some(found) = results.into_iter().find(|item| item.exists) {
                    return Ok(UploadResult {
                        backup_id: found.backup_id.unwrap_or_default(),
                        size_bytes: found.size_bytes.unwrap_or_default(),
                        etag: None,
                        quota: None,
                        skipped: true,
                    });
                }
            }
        }

        let presign = self
            .presign_upload(PresignUploadInput {
                address,
                file_size: size,
                content_type: content_type.clone(),
                content_hash,
            })
            .await?;

        let stream = ReaderStream::new(ProgressReader::new(reader, size, options.progress));
        let response = self
            .http
            .put(&presign.presigned_url)
            .header(reqwest::header::CONTENT_TYPE, content_type)
            .body(Body::wrap_stream(stream))
            .send()
            .await?;
        let status = response.status();
        if !status.is_success() {
            return Err(UmbraError::storage(status, "object storage upload failed"));
        }

        let confirmed = self
            .confirm_upload(BackupTarget::id(presign.backup_id))
            .await?;
        Ok(UploadResult {
            backup_id: confirmed.backup_id,
            size_bytes: confirmed.size_bytes,
            etag: Some(confirmed.etag),
            quota: Some(confirmed.quota),
            skipped: false,
        })
    }

    pub async fn download_file(
        &self,
        target: BackupTarget,
        path: impl AsRef<Path>,
        options: DownloadOptions,
    ) -> Result<DownloadResult, UmbraError> {
        let path = path.as_ref();
        if !options.overwrite && tokio::fs::try_exists(path).await? {
            return Err(UmbraError::invalid_input("target file already exists"));
        }
        if let Some(parent) = path.parent() {
            if !parent.as_os_str().is_empty() {
                tokio::fs::create_dir_all(parent).await?;
            }
        }
        let tmp = path.with_extension(format!(
            "{}tmp",
            path.extension()
                .and_then(|ext| ext.to_str())
                .map(|ext| format!("{ext}."))
                .unwrap_or_default()
        ));
        let mut file = File::create(&tmp).await?;
        let result = self
            .download_writer(target, &mut file, DownloadOptions { ..options })
            .await?;
        file.flush().await?;
        drop(file);
        tokio::fs::rename(&tmp, path).await?;
        Ok(result)
    }

    pub async fn download_writer<W>(
        &self,
        target: BackupTarget,
        writer: &mut W,
        options: DownloadOptions,
    ) -> Result<DownloadResult, UmbraError>
    where
        W: AsyncWrite + Unpin + Send,
    {
        let presign = self.presign_download(target).await?;
        let mut response = self.http.get(&presign.presigned_url).send().await?;
        let status = response.status();
        if !status.is_success() {
            return Err(UmbraError::storage(
                status,
                "object storage download failed",
            ));
        }
        let mut done = 0_u64;
        while let Some(chunk) = response.chunk().await? {
            done += chunk.len() as u64;
            writer.write_all(&chunk).await?;
            if let Some(progress) = options.progress.as_ref() {
                progress(done, presign.size_bytes);
            }
        }
        Ok(DownloadResult {
            backup_id: presign.backup_id,
            size_bytes: presign.size_bytes,
            etag: presign.etag,
        })
    }
}

fn make_presign_request(input: PresignUploadInput) -> Result<PresignUploadRequest, UmbraError> {
    input.address.validate()?;
    if input.file_size == 0 {
        return Err(UmbraError::invalid_input(
            "file_size must be greater than zero",
        ));
    }
    if input.content_type.trim().is_empty() {
        return Err(UmbraError::invalid_input("content_type is required"));
    }
    Ok(PresignUploadRequest {
        category: input.address.category.as_str().to_string(),
        subject: input.address.subject.unwrap_or_default(),
        version: input.address.version,
        file_size: input.file_size,
        content_type: input.content_type,
        content_hash: normalize_content_hash(input.content_hash, true)?,
    })
}

fn make_target_request(target: BackupTarget) -> Result<BackupTargetRequest, UmbraError> {
    if let Some(backup_id) = target.backup_id {
        return Ok(BackupTargetRequest {
            backup_id: Some(backup_id),
            category: None,
            subject: None,
            version: None,
        });
    }
    let address = target
        .address
        .ok_or_else(|| UmbraError::invalid_input("backup_id or address is required"))?;
    address.validate()?;
    Ok(BackupTargetRequest {
        backup_id: None,
        category: Some(address.category.as_str().to_string()),
        subject: address.subject,
        version: Some(address.version),
    })
}

fn normalize_content_hash(
    hash: Option<String>,
    allow_empty: bool,
) -> Result<Option<String>, UmbraError> {
    let Some(hash) = hash else {
        if allow_empty {
            return Ok(None);
        }
        return Err(UmbraError::invalid_input("content_hash is required"));
    };
    let hash = hash.trim().to_ascii_lowercase();
    if hash.is_empty() {
        if allow_empty {
            return Ok(None);
        }
        return Err(UmbraError::invalid_input("content_hash is required"));
    }
    let re = Regex::new(r"^[a-f0-9]{64}$").expect("valid regex");
    if !re.is_match(&hash) {
        return Err(UmbraError::invalid_input(
            "content_hash must be lowercase SHA-256 hex",
        ));
    }
    Ok(Some(hash))
}

struct ProgressReader<R> {
    inner: R,
    done: u64,
    total: u64,
    progress: Option<Arc<dyn Fn(u64, u64) + Send + Sync>>,
}

impl<R> ProgressReader<R> {
    fn new(inner: R, total: u64, progress: Option<Arc<dyn Fn(u64, u64) + Send + Sync>>) -> Self {
        Self {
            inner,
            done: 0,
            total,
            progress,
        }
    }
}

impl<R> AsyncRead for ProgressReader<R>
where
    R: AsyncRead + Unpin,
{
    fn poll_read(
        mut self: Pin<&mut Self>,
        cx: &mut std::task::Context<'_>,
        buf: &mut tokio::io::ReadBuf<'_>,
    ) -> std::task::Poll<std::io::Result<()>> {
        let before = buf.filled().len();
        let pinned = unsafe { self.as_mut().map_unchecked_mut(|s| &mut s.inner) };
        match pinned.poll_read(cx, buf) {
            std::task::Poll::Ready(Ok(())) => {
                let after = buf.filled().len();
                if after > before {
                    self.done += (after - before) as u64;
                    if let Some(progress) = self.progress.as_ref() {
                        progress(self.done, self.total);
                    }
                }
                std::task::Poll::Ready(Ok(()))
            }
            other => other,
        }
    }
}
