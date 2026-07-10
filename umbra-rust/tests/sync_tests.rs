use chrono::{Duration, Utc};
use httpmock::{
    Method::{GET, POST},
    MockServer,
};
use serde_json::json;
use umbra_sdk::{
    canonical_string, request_body_hash, sign_canonical_string, DeviceCredential,
    DeviceCredentialStore, MemoryDeviceCredentialStore, MemoryTokenStore, NoopBrowserOpener,
    SyncExchangeInput, SyncMutation, SyncRecordKey, SyncSnapshotInput, TokenSet, TokenStore,
    UmbraClient,
};

#[tokio::test]
async fn exchange_and_snapshot_are_device_signed() {
    let server = MockServer::start_async().await;
    let exchange = server
        .mock_async(|when, then| {
            when.method(POST)
                .path("/api/v1/client/sync/exchange")
                .header("authorization", "Bearer token")
                .is_true(|req| verify_signature(req, "device-secret", "device-1"));
            then.status(200).json_body(json!({
                "code": 0,
                "msg": "success",
                "data": {
                    "accepted": [],
                    "conflicts": [{"mutation_id": "m-1", "reason": "base_version_mismatch"}],
                    "rejected": [],
                    "changes": [],
                    "next_cursor": "cursor-1",
                    "has_more": false,
                    "reset_required": false
                }
            }));
        })
        .await;
    let snapshot = server
        .mock_async(|when, then| {
            when.method(GET)
                .path("/api/v1/client/sync/snapshot")
                .query_param("protocol_version", "1")
                .query_param("space", "library")
                .query_param("cursor", "page-1")
                .query_param("limit", "25")
                .is_true(|req| verify_signature(req, "device-secret", "device-1"));
            then.status(200).json_body(json!({
                "code": 0,
                "msg": "success",
                "data": {
                    "records": [],
                    "exchange_cursor": "cursor-2",
                    "has_more": false
                }
            }));
        })
        .await;

    let client = signed_client(&server).await;
    let key = SyncRecordKey {
        namespace: "lunabox.library".to_string(),
        collection: "games".to_string(),
        record_id: "game-1".to_string(),
    };
    let mut input = SyncExchangeInput::new("library");
    input.mutations.push(
        SyncMutation::upsert("m-1", key, 1, 1, json!({"name": "Example"})).expect("mutation"),
    );
    let result = client.sync().exchange(input).await.expect("exchange");
    assert_eq!(result.conflicts.len(), 1);
    assert_eq!(result.conflicts[0].mutation_id, "m-1");

    let mut snapshot_input = SyncSnapshotInput::new("library");
    snapshot_input.cursor = Some("page-1".to_string());
    snapshot_input.limit = Some(25);
    let page = client
        .sync()
        .snapshot(snapshot_input)
        .await
        .expect("snapshot");
    assert_eq!(page.exchange_cursor, "cursor-2");

    exchange.assert_async().await;
    snapshot.assert_async().await;
}

async fn signed_client(server: &MockServer) -> UmbraClient {
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
            device_id: "device-1".to_string(),
            device_secret: "device-secret".to_string(),
        })
        .await
        .expect("save device");
    let config = UmbraClient::builder()
        .base_url(server.base_url())
        .client_id("client")
        .redirect_uri("http://127.0.0.1:0/auth/callback")
        .token_store(token_store)
        .device_store_arc(device_store)
        .browser_opener(NoopBrowserOpener)
        .build()
        .expect("config");
    UmbraClient::new(config).expect("client")
}

fn verify_signature(req: &httpmock::HttpMockRequest, secret: &str, device_id: &str) -> bool {
    let headers = req.headers();
    if headers
        .get("x-umbra-device-id")
        .and_then(|value| value.to_str().ok())
        != Some(device_id)
    {
        return false;
    }
    let timestamp = match headers
        .get("x-umbra-timestamp")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.parse::<i64>().ok())
    {
        Some(value) => value,
        None => return false,
    };
    let nonce = match headers
        .get("x-umbra-nonce")
        .and_then(|value| value.to_str().ok())
    {
        Some(value) => value,
        None => return false,
    };
    let body_hash = request_body_hash(req.body_ref());
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
        && headers
            .get("x-umbra-body-sha256")
            .and_then(|value| value.to_str().ok())
            == Some(body_hash.as_str())
}
