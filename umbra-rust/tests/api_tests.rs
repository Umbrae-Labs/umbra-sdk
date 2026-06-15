use chrono::{Duration, Utc};
use httpmock::{
    Method::{GET, POST},
    MockServer,
};
use serde_json::json;
use umbra_sdk::{MemoryTokenStore, NoopBrowserOpener, TokenSet, TokenStore, UmbraClient};

#[tokio::test]
async fn refreshes_and_retries_on_invalid_token() {
    let server = MockServer::start_async().await;

    let first_quota = server
        .mock_async(|when, then| {
            when.method(GET)
                .path("/api/v1/user/quota")
                .header("authorization", "Bearer old-token");
            then.status(401)
                .json_body(json!({ "code": 1004, "msg": "Token invalid", "data": null }));
        })
        .await;

    let token = server
        .mock_async(|when, then| {
            when.method(POST).path("/oauth2/token");
            then.status(200).json_body(json!({
                "access_token": "new-token",
                "refresh_token": "refresh-token",
                "token_type": "bearer",
                "expires_in": 3600
            }));
        })
        .await;

    let second_quota = server
        .mock_async(|when, then| {
            when.method(GET)
                .path("/api/v1/user/quota")
                .header("authorization", "Bearer new-token");
            then.status(200).json_body(json!({
                "code": 0,
                "msg": "success",
                "data": { "quota_bytes": 10, "used_bytes": 3, "available_bytes": 7 }
            }));
        })
        .await;

    let store = MemoryTokenStore::default();
    store
        .save(&TokenSet {
            access_token: "old-token".to_string(),
            refresh_token: Some("refresh-token".to_string()),
            token_type: "bearer".to_string(),
            scope: None,
            expires_at: Some(Utc::now() + Duration::hours(1)),
        })
        .await
        .expect("save token");

    let config = UmbraClient::builder()
        .base_url(server.base_url())
        .client_id("client")
        .redirect_uri("http://127.0.0.1:0/auth/callback")
        .token_store(store)
        .browser_opener(NoopBrowserOpener)
        .build()
        .expect("config");
    let client = UmbraClient::new(config).expect("client");
    let quota = client.user().quota().await.expect("quota");

    assert_eq!(quota.available_bytes, 7);
    first_quota.assert_async().await;
    token.assert_async().await;
    second_quota.assert_async().await;
}
