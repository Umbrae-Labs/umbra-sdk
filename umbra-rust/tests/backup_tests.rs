use chrono::{Duration, Utc};
use httpmock::{
    Method::{GET, POST, PUT},
    MockServer,
};
use serde_json::json;
use umbra_sdk::{
    canonical_string, request_body_hash, sign_canonical_string, BackupAddress, BackupCategory,
    BackupListFilter, DeviceCredential, DeviceCredentialStore, MemoryDeviceCredentialStore,
    MemoryTokenStore, NoopBrowserOpener, TokenSet, TokenStore, UmbraClient, UploadOptions,
};

#[tokio::test]
async fn upload_reader_presign_put_confirm() {
    let api = MockServer::start_async().await;
    let object = MockServer::start_async().await;

    let put = object
        .mock_async(|when, then| {
            when.method(PUT).path("/object").body("hello");
            then.status(200);
        })
        .await;

    let presign = api
        .mock_async(|when, then| {
            when.method(POST)
                .path("/api/v1/client/backup/presign")
                .header("X-Umbra-Device-Id", "dev_test_123")
                .is_true(|req| verify_device_signature(req, "test-device-secret", "dev_test_123"))
                .json_body(json!({
                    "category": "game",
                    "subject": "mc",
                    "version": "v1",
                    "file_size": 5,
                    "content_type": "text/plain"
                }));
            then.status(200).json_body(json!({
                "code": 0,
                "msg": "success",
                "data": {
                    "backup_id": 42,
                    "presigned_url": format!("{}/object", object.base_url()),
                    "expires_in": 3600
                }
            }));
        })
        .await;

    let confirm = api
        .mock_async(|when, then| {
            when.method(POST)
                .path("/api/v1/client/backup/confirm")
                .header("X-Umbra-Device-Id", "dev_test_123")
                .is_true(|req| verify_device_signature(req, "test-device-secret", "dev_test_123"))
                .json_body(json!({ "backup_id": 42 }));
            then.status(200).json_body(json!({
                "code": 0,
                "msg": "success",
                "data": {
                    "backup_id": 42,
                    "size_bytes": 5,
                    "etag": "etag",
                    "quota": { "quota_bytes": 10, "used_bytes": 5, "available_bytes": 5 }
                }
            }));
        })
        .await;

    let store = MemoryTokenStore::default();
    store
        .save(&TokenSet {
            access_token: "token".to_string(),
            refresh_token: None,
            token_type: "bearer".to_string(),
            scope: None,
            expires_at: Some(Utc::now() + Duration::hours(1)),
        })
        .await
        .expect("save token");
    let device_store = std::sync::Arc::new(MemoryDeviceCredentialStore::default());
    device_store
        .save(&DeviceCredential {
            device_id: "dev_test_123".to_string(),
            device_secret: "test-device-secret".to_string(),
        })
        .await
        .expect("save device credential");

    let config = UmbraClient::builder()
        .base_url(api.base_url())
        .client_id("client")
        .redirect_uri("http://127.0.0.1:0/auth/callback")
        .token_store(store)
        .device_store_arc(device_store)
        .browser_opener(NoopBrowserOpener)
        .build()
        .expect("config");
    let client = UmbraClient::new(config).expect("client");

    let result = client
        .backups()
        .upload_reader(
            BackupAddress::game("mc", "v1"),
            std::io::Cursor::new(b"hello".to_vec()),
            5,
            UploadOptions {
                content_type: Some("text/plain".to_string()),
                ..Default::default()
            },
        )
        .await
        .expect("upload");

    assert_eq!(result.backup_id, 42);
    assert_eq!(result.size_bytes, 5);
    presign.assert_async().await;
    put.assert_async().await;
    confirm.assert_async().await;
}

#[tokio::test]
async fn backup_list_signs_empty_body_hash_and_query() {
    let api = MockServer::start_async().await;
    let empty_body_hash = request_body_hash(&[]);

    let list = api
        .mock_async(|when, then| {
            when.method(GET)
                .path("/api/v1/client/backup/list")
                .query_param("category", "game")
                .query_param("subject", "mc")
                .header("X-Umbra-Device-Id", "dev_test_123")
                .header("X-Umbra-Body-SHA256", empty_body_hash.as_str())
                .is_true(|req| verify_device_signature(req, "test-device-secret", "dev_test_123"));
            then.status(200).json_body(json!({
                "code": 0,
                "msg": "success",
                "data": { "files": [], "total": 0 }
            }));
        })
        .await;

    let client = test_client_with_device(&api).await;
    let files = client
        .backups()
        .list(BackupListFilter {
            category: Some(BackupCategory::Game),
            subject: Some("mc".to_string()),
        })
        .await
        .expect("list backups");

    assert!(files.is_empty());
    list.assert_async().await;
}

async fn test_client_with_device(api: &MockServer) -> UmbraClient {
    let token_store = MemoryTokenStore::default();
    token_store
        .save(&TokenSet {
            access_token: "token".to_string(),
            refresh_token: None,
            token_type: "bearer".to_string(),
            scope: None,
            expires_at: Some(Utc::now() + Duration::hours(1)),
        })
        .await
        .expect("save token");
    let device_store = std::sync::Arc::new(MemoryDeviceCredentialStore::default());
    device_store
        .save(&DeviceCredential {
            device_id: "dev_test_123".to_string(),
            device_secret: "test-device-secret".to_string(),
        })
        .await
        .expect("save device credential");

    let config = UmbraClient::builder()
        .base_url(api.base_url())
        .client_id("client")
        .redirect_uri("http://127.0.0.1:0/auth/callback")
        .token_store(token_store)
        .device_store_arc(device_store)
        .browser_opener(NoopBrowserOpener)
        .build()
        .expect("config");
    UmbraClient::new(config).expect("client")
}

fn verify_device_signature(req: &httpmock::HttpMockRequest, secret: &str, device_id: &str) -> bool {
    let headers = req.headers();
    let timestamp = match headers
        .get("x-umbra-timestamp")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.parse::<i64>().ok())
    {
        Some(timestamp) => timestamp,
        None => return false,
    };
    let nonce = match headers
        .get("x-umbra-nonce")
        .and_then(|value| value.to_str().ok())
    {
        Some(nonce) => nonce,
        None => return false,
    };
    let body_hash = request_body_hash(req.body_ref());
    if headers
        .get("x-umbra-body-sha256")
        .and_then(|value| value.to_str().ok())
        != Some(body_hash.as_str())
    {
        return false;
    }
    let canonical = canonical_string(
        req.method_str(),
        req.uri()
            .path_and_query()
            .map(|value| value.as_str())
            .unwrap_or(req.uri().path()),
        timestamp,
        nonce,
        &body_hash,
        device_id,
    );
    let expected = format!("v1={}", sign_canonical_string(secret, &canonical));
    headers
        .get("x-umbra-signature")
        .and_then(|value| value.to_str().ok())
        == Some(expected.as_str())
}
