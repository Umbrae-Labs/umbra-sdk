use std::sync::Arc;

use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine as _};
use chrono::{DateTime, Duration as ChronoDuration, Utc};
use rand::RngCore;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tokio::sync::Mutex;
use url::Url;

use crate::{
    config::Endpoints, CallbackReceiver, LoopbackCallbackReceiver, UmbraConfig, UmbraError,
};

#[derive(Clone)]
pub struct AuthClient {
    config: UmbraConfig,
    endpoints: Endpoints,
    refresh_lock: Arc<Mutex<()>>,
}

#[derive(Debug, Clone)]
pub struct Session {
    pub token: TokenSet,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TokenSet {
    pub access_token: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub refresh_token: Option<String>,
    pub token_type: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub scope: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Default)]
pub struct AuthCallback {
    pub code: String,
    pub state: String,
    pub error: Option<String>,
}

#[derive(Debug, Deserialize)]
struct HydraTokenResponse {
    access_token: String,
    #[serde(default)]
    token_type: String,
    #[serde(default)]
    refresh_token: Option<String>,
    #[serde(default)]
    expires_in: Option<i64>,
    #[serde(default)]
    scope: Option<String>,
}

impl AuthClient {
    pub(crate) fn new(config: UmbraConfig, endpoints: Endpoints) -> Self {
        Self {
            config,
            endpoints,
            refresh_lock: Arc::new(Mutex::new(())),
        }
    }

    pub async fn login(&self) -> Result<Session, UmbraError> {
        let (verifier, challenge) = new_pkce();
        let state = random_hex(16);

        let mut redirect_uri = self.config.redirect_uri.clone();
        let receiver_holder;
        let receiver: Arc<dyn CallbackReceiver> =
            if let Some(receiver) = self.config.callback_receiver.clone() {
                receiver
            } else {
                let loopback = LoopbackCallbackReceiver::bind(&redirect_uri).await?;
                redirect_uri = loopback.redirect_uri().to_owned();
                receiver_holder = Arc::new(loopback);
                receiver_holder
            };

        let authorize_url = self.authorize_url(&redirect_uri, &state, &challenge)?;
        self.config
            .browser_opener
            .open_url(authorize_url.as_str())
            .await?;

        let callback = receiver.receive(&state).await?;
        if let Some(error) = callback.error {
            return Err(UmbraError::auth(format!("authorization failed: {error}")));
        }
        if callback.code.is_empty() {
            return Err(UmbraError::auth("authorization callback missing code"));
        }

        let token = self
            .exchange_code(&callback.code, &verifier, &redirect_uri)
            .await?;
        self.config.token_store.save(&token).await?;
        Ok(Session { token })
    }

    pub async fn refresh(&self) -> Result<TokenSet, UmbraError> {
        let _guard = self.refresh_lock.lock().await;
        let current = self
            .config
            .token_store
            .load()
            .await?
            .ok_or_else(|| UmbraError::auth("not authenticated"))?;
        let refresh_token = current
            .refresh_token
            .clone()
            .ok_or_else(|| UmbraError::auth("refresh token is not available"))?;

        let params = [
            ("grant_type", "refresh_token"),
            ("client_id", self.config.client_id.as_str()),
            ("refresh_token", refresh_token.as_str()),
        ];
        let mut token = self.token_request(&params).await?;
        if token.refresh_token.is_none() {
            token.refresh_token = Some(refresh_token);
        }
        self.config.token_store.save(&token).await?;
        Ok(token)
    }

    pub async fn logout(&self) -> Result<(), UmbraError> {
        if let Some(token) = self.config.token_store.load().await? {
            if let Some(refresh_token) = token.refresh_token.as_deref() {
                let _ = self.revoke(refresh_token, "refresh_token").await;
            }
            if !token.access_token.is_empty() {
                let _ = self.revoke(&token.access_token, "access_token").await;
            }
        }
        self.config.token_store.clear().await
    }

    pub async fn token(&self) -> Result<TokenSet, UmbraError> {
        let token = self
            .config
            .token_store
            .load()
            .await?
            .ok_or_else(|| UmbraError::auth("not authenticated"))?;
        if !self.should_refresh(&token) {
            return Ok(token);
        }
        if token.refresh_token.is_none() {
            return Ok(token);
        }
        self.refresh().await
    }

    pub async fn is_authenticated(&self) -> bool {
        self.token()
            .await
            .map(|t| !t.access_token.is_empty())
            .unwrap_or(false)
    }

    fn authorize_url(
        &self,
        redirect_uri: &str,
        state: &str,
        challenge: &str,
    ) -> Result<Url, UmbraError> {
        let mut url = Url::parse(&self.endpoints.authorization_endpoint)
            .map_err(|_| UmbraError::invalid_input("authorization endpoint is invalid"))?;
        url.query_pairs_mut()
            .append_pair("response_type", "code")
            .append_pair("client_id", &self.config.client_id)
            .append_pair("redirect_uri", redirect_uri)
            .append_pair("scope", &self.config.scope)
            .append_pair("state", state)
            .append_pair("code_challenge", challenge)
            .append_pair("code_challenge_method", "S256");
        Ok(url)
    }

    async fn exchange_code(
        &self,
        code: &str,
        verifier: &str,
        redirect_uri: &str,
    ) -> Result<TokenSet, UmbraError> {
        let params = [
            ("grant_type", "authorization_code"),
            ("client_id", self.config.client_id.as_str()),
            ("code", code),
            ("redirect_uri", redirect_uri),
            ("code_verifier", verifier),
        ];
        self.token_request(&params).await
    }

    async fn token_request(&self, params: &[(&str, &str)]) -> Result<TokenSet, UmbraError> {
        let response = self
            .config
            .http_client
            .post(&self.endpoints.token_endpoint)
            .form(params)
            .send()
            .await?;
        let status = response.status();
        let bytes = response.bytes().await?;
        if !status.is_success() {
            return Err(UmbraError::Api {
                status: Some(status),
                code: None,
                kind: crate::ErrorKind::Auth,
                message: format!(
                    "token endpoint returned {}: {}",
                    status,
                    String::from_utf8_lossy(&bytes)
                ),
            });
        }
        let body: HydraTokenResponse = serde_json::from_slice(&bytes)?;
        if body.access_token.is_empty() {
            return Err(UmbraError::auth("token response missing access_token"));
        }
        let expires_at = body
            .expires_in
            .map(|seconds| Utc::now() + ChronoDuration::seconds(seconds));
        Ok(TokenSet {
            access_token: body.access_token,
            refresh_token: body.refresh_token,
            token_type: if body.token_type.is_empty() {
                "bearer".to_string()
            } else {
                body.token_type
            },
            scope: body.scope,
            expires_at,
        })
    }

    async fn revoke(&self, token: &str, token_type_hint: &str) -> Result<(), UmbraError> {
        let params = [
            ("token", token),
            ("token_type_hint", token_type_hint),
            ("client_id", self.config.client_id.as_str()),
        ];
        let response = self
            .config
            .http_client
            .post(&self.endpoints.revocation_endpoint)
            .form(&params)
            .send()
            .await?;
        if !response.status().is_success() {
            return Err(UmbraError::auth("token revocation failed"));
        }
        Ok(())
    }

    fn should_refresh(&self, token: &TokenSet) -> bool {
        let Some(expires_at) = token.expires_at else {
            return false;
        };
        let skew = ChronoDuration::from_std(self.config.refresh_skew)
            .unwrap_or_else(|_| ChronoDuration::seconds(60));
        Utc::now() + skew >= expires_at
    }
}

fn new_pkce() -> (String, String) {
    let mut bytes = [0_u8; 48];
    rand::thread_rng().fill_bytes(&mut bytes);
    let verifier = URL_SAFE_NO_PAD.encode(bytes);
    let challenge = URL_SAFE_NO_PAD.encode(Sha256::digest(verifier.as_bytes()));
    (verifier, challenge)
}

fn random_hex(len: usize) -> String {
    let mut bytes = vec![0_u8; len];
    rand::thread_rng().fill_bytes(&mut bytes);
    hex::encode(bytes)
}
