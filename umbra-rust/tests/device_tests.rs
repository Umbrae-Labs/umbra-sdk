use chrono::{Duration, Utc};
use httpmock::{Method::POST, MockServer};
use serde_json::json;
use umbra_sdk::{
    canonical_string, detect_windows_device_metadata, parse_registration_token, request_body_hash,
    sign_canonical_string, DeviceCredential, DeviceCredentialStore, DeviceRegistrationInput,
    MemoryDeviceCredentialStore, MemoryTokenStore, NoopBrowserOpener, TokenSet, TokenStore,
    UmbraClient, WindowsDeviceMetadataOptions,
};

#[cfg(windows)]
#[tokio::test]
async fn registers_device_with_registration_signature_and_saves_credential() {
    let server = MockServer::start_async().await;

    let register = server
        .mock_async(|when, then| {
            when.method(POST)
                .path("/api/v1/client/devices/register")
                .header("authorization", "Bearer token")
                .header_missing("x-umbra-device-id")
                .is_true(|req| verify_signature(req, "registration-secret", "registration"))
                .is_true(|req| {
                    let Ok(body) = serde_json::from_slice::<serde_json::Value>(req.body_ref())
                    else {
                        return false;
                    };
                    body.get("credential_id").and_then(|value| value.as_str()) == Some("ucd_test")
                        && body
                            .get("device")
                            .and_then(|device| device.get("name"))
                            .and_then(|value| value.as_str())
                            .map(|name| !name.trim().is_empty())
                            .unwrap_or(false)
                });
            then.status(200).json_body(json!({
                "code": 0,
                "msg": "success",
                "data": {
                    "device": {
                        "device_id": "dev_registered",
                        "name": "LunaBook",
                        "platform": "darwin",
                        "status": 0
                    },
                    "device_secret": "device-secret-value",
                    "secret_once": true
                }
            }));
        })
        .await;

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

    let config = UmbraClient::builder()
        .base_url(server.base_url())
        .client_id("client")
        .redirect_uri("http://127.0.0.1:0/auth/callback")
        .token_store(token_store)
        .device_store_arc(device_store.clone())
        .browser_opener(NoopBrowserOpener)
        .build()
        .expect("config");
    let client = UmbraClient::new(config).expect("client");
    let device = detect_windows_device_metadata(WindowsDeviceMetadataOptions::default())
        .expect("detect device metadata");

    let result = client
        .devices()
        .register(DeviceRegistrationInput::with_credential(
            "ucd_test",
            "registration-secret",
            device,
        ))
        .await
        .expect("register device");

    assert_eq!(result.device.device_id, "dev_registered");
    assert_eq!(result.device_secret, "device-secret-value");
    assert!(result.secret_once);
    let saved = device_store
        .load()
        .await
        .expect("load device credential")
        .expect("saved credential");
    assert_eq!(saved.device_id, "dev_registered");
    assert_eq!(saved.device_secret, "device-secret-value");
    register.assert_async().await;
}

#[test]
fn signs_documented_device_signature_vector() {
    let body = br#"{"name":"LunaBox"}"#;
    let body_hash = request_body_hash(body);
    assert_eq!(body_hash, "njjTtBgg9nmsDBrctFvuSK8L6lsXJW7eeRFtarJC20M");

    let canonical = canonical_string(
        "POST",
        "/api/v1/client/backup/presign?category=world&version=42",
        1716200000,
        "nonce-001",
        &body_hash,
        "dev_test_123",
    );
    assert_eq!(
        sign_canonical_string("test-device-secret", &canonical),
        "HGr3hoz1CHqufCk3xd43kwfHBi3XhTMTtfKpVy2POZA"
    );
}

#[tokio::test]
async fn rotates_device_secret_and_saves_replacement() {
    let server = MockServer::start_async().await;

    let rotate = server
        .mock_async(|when, then| {
            when.method(POST)
                .path("/api/v1/user/devices/dev_registered/rotate-secret")
                .header("authorization", "Bearer token");
            then.status(200).json_body(json!({
                "code": 0,
                "msg": "success",
                "data": {
                    "device": {
                        "device_id": "dev_registered",
                        "name": "LunaBook",
                        "status": 0
                    },
                    "device_secret": "new-device-secret",
                    "secret_once": true
                }
            }));
        })
        .await;

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
            device_id: "dev_registered".to_string(),
            device_secret: "old-device-secret".to_string(),
        })
        .await
        .expect("save device");

    let config = UmbraClient::builder()
        .base_url(server.base_url())
        .client_id("client")
        .redirect_uri("http://127.0.0.1:0/auth/callback")
        .token_store(token_store)
        .device_store_arc(device_store.clone())
        .browser_opener(NoopBrowserOpener)
        .build()
        .expect("config");
    let client = UmbraClient::new(config).expect("client");

    let result = client
        .devices()
        .rotate_secret(None)
        .await
        .expect("rotate secret");

    assert_eq!(result.device.device_id, "dev_registered");
    assert_eq!(result.device_secret, "new-device-secret");
    let saved = device_store
        .load()
        .await
        .expect("load device credential")
        .expect("saved credential");
    assert_eq!(saved.device_secret, "new-device-secret");
    rotate.assert_async().await;
}

#[test]
fn parses_registration_token_like_server() {
    assert_eq!(
        parse_registration_token("umbra_reg_v1_ucd_test.secret-value").unwrap(),
        ("ucd_test".to_string(), "secret-value".to_string())
    );
    assert!(parse_registration_token("umbra_reg_v1_ucd_test.secret.value").is_err());
}

fn verify_signature(req: &httpmock::HttpMockRequest, secret: &str, device_id: &str) -> bool {
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
