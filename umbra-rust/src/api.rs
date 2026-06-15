use serde::{de::DeserializeOwned, Deserialize, Serialize};
use url::Url;

use crate::{
    config::join_url,
    device::{
        canonical_string, random_nonce, request_body_hash, sign_canonical_string, DeviceCredential,
        REGISTRATION_DEVICE_ID,
    },
    store::DeviceCredentialStore,
    AuthClient, UmbraError,
};

#[derive(Clone)]
pub(crate) struct ApiClient {
    http: reqwest::Client,
    base_url: String,
    auth: AuthClient,
    device_store: std::sync::Arc<dyn DeviceCredentialStore>,
}

#[derive(Debug, Deserialize)]
struct Envelope<T> {
    code: i32,
    msg: String,
    data: Option<T>,
}

impl ApiClient {
    pub(crate) fn new(
        http: reqwest::Client,
        base_url: String,
        auth: AuthClient,
        device_store: std::sync::Arc<dyn DeviceCredentialStore>,
    ) -> Self {
        Self {
            http,
            base_url,
            auth,
            device_store,
        }
    }

    pub(crate) async fn get<T>(&self, path: &str, query: &[(&str, String)]) -> Result<T, UmbraError>
    where
        T: DeserializeOwned,
    {
        self.send_json::<(), T>(reqwest::Method::GET, path, None, query, true)
            .await
    }

    pub(crate) async fn post<I, T>(&self, path: &str, body: &I) -> Result<T, UmbraError>
    where
        I: Serialize + ?Sized,
        T: DeserializeOwned,
    {
        self.send_json(reqwest::Method::POST, path, Some(body), &[], true)
            .await
    }

    pub(crate) async fn delete<I, T>(&self, path: &str, body: &I) -> Result<T, UmbraError>
    where
        I: Serialize + ?Sized,
        T: DeserializeOwned,
    {
        self.send_json(reqwest::Method::DELETE, path, Some(body), &[], true)
            .await
    }

    pub(crate) async fn post_registration_signed<I, T>(
        &self,
        path: &str,
        body: &I,
        registration_secret: &str,
    ) -> Result<T, UmbraError>
    where
        I: Serialize + ?Sized,
        T: DeserializeOwned,
    {
        self.send_json_signed(
            reqwest::Method::POST,
            path,
            Some(body),
            &[],
            SigningSecret::Registration(registration_secret),
            true,
        )
        .await
    }

    pub(crate) async fn load_device_credential(
        &self,
    ) -> Result<Option<DeviceCredential>, UmbraError> {
        self.device_store.load().await
    }

    pub(crate) async fn save_device_credential(
        &self,
        credential: DeviceCredential,
    ) -> Result<(), UmbraError> {
        self.device_store.save(&credential).await
    }

    pub(crate) async fn clear_device_credential(&self) -> Result<(), UmbraError> {
        self.device_store.clear().await
    }

    async fn send_json<I, T>(
        &self,
        method: reqwest::Method,
        path: &str,
        body: Option<&I>,
        query: &[(&str, String)],
        retry_auth: bool,
    ) -> Result<T, UmbraError>
    where
        I: Serialize + ?Sized,
        T: DeserializeOwned,
    {
        if is_device_protected_path(path) {
            return self
                .send_json_signed(
                    method,
                    path,
                    body,
                    query,
                    SigningSecret::StoredDevice,
                    retry_auth,
                )
                .await;
        }

        let token = self.auth.token().await?;
        let url = self.build_url(path, query)?;
        let mut request = self
            .http
            .request(method.clone(), url)
            .bearer_auth(token.access_token)
            .header(reqwest::header::ACCEPT, "application/json");
        if let Some(body) = body {
            request = request
                .header(reqwest::header::CONTENT_TYPE, "application/json")
                .body(serde_json::to_vec(body)?);
        }
        let response = request.send().await?;
        let result = decode_envelope::<T>(response).await;
        if retry_auth && matches!(result, Err(ref err) if err.is_invalid_token()) {
            let _ = self.auth.refresh().await?;
            return Box::pin(self.send_json(method, path, body, query, false)).await;
        }
        result
    }

    async fn send_json_signed<I, T>(
        &self,
        method: reqwest::Method,
        path: &str,
        body: Option<&I>,
        query: &[(&str, String)],
        signing_secret: SigningSecret<'_>,
        retry_auth: bool,
    ) -> Result<T, UmbraError>
    where
        I: Serialize + ?Sized,
        T: DeserializeOwned,
    {
        let token = self.auth.token().await?;
        let url = self.build_url(path, query)?;
        let path_with_query = path_with_query(&url);
        let body_bytes = match body {
            Some(body) => serde_json::to_vec(body)?,
            None => Vec::new(),
        };
        let body_hash = request_body_hash(&body_bytes);
        let timestamp = current_unix_timestamp()?;
        let nonce = random_nonce();
        let (device_id, secret, include_device_id_header) = match signing_secret {
            SigningSecret::StoredDevice => {
                let credential = self
                    .device_store
                    .load()
                    .await?
                    .ok_or_else(|| UmbraError::auth("device credentials are not available"))?;
                (credential.device_id, credential.device_secret, true)
            }
            SigningSecret::Registration(secret) => {
                (REGISTRATION_DEVICE_ID.to_string(), secret.to_owned(), false)
            }
        };
        let canonical = canonical_string(
            method.as_str(),
            &path_with_query,
            timestamp,
            &nonce,
            &body_hash,
            &device_id,
        );
        let signature = sign_canonical_string(&secret, &canonical);
        let mut request = self
            .http
            .request(method.clone(), url)
            .bearer_auth(token.access_token)
            .header(reqwest::header::ACCEPT, "application/json")
            .header("X-Umbra-Timestamp", timestamp.to_string())
            .header("X-Umbra-Nonce", nonce)
            .header("X-Umbra-Body-SHA256", body_hash)
            .header("X-Umbra-Signature", format!("v1={signature}"));
        if include_device_id_header {
            request = request.header("X-Umbra-Device-Id", device_id);
        }
        if body.is_some() {
            request = request
                .header(reqwest::header::CONTENT_TYPE, "application/json")
                .body(body_bytes);
        }
        let response = request.send().await?;
        let result = decode_envelope::<T>(response).await;
        if retry_auth && matches!(result, Err(ref err) if err.is_invalid_token()) {
            let _ = self.auth.refresh().await?;
            return Box::pin(self.send_json_signed(
                method,
                path,
                body,
                query,
                signing_secret,
                false,
            ))
            .await;
        }
        result
    }

    fn build_url(&self, path: &str, query: &[(&str, String)]) -> Result<Url, UmbraError> {
        let mut url = Url::parse(&join_url(&self.base_url, path))
            .map_err(|_| UmbraError::invalid_input("request URL is invalid"))?;
        if !query.is_empty() {
            url.query_pairs_mut()
                .extend_pairs(query.iter().map(|(k, v)| (*k, v.as_str())));
        }
        Ok(url)
    }
}

#[derive(Clone, Copy)]
enum SigningSecret<'a> {
    StoredDevice,
    Registration(&'a str),
}

fn is_device_protected_path(path: &str) -> bool {
    path.starts_with("/client/backup/")
}

fn path_with_query(url: &Url) -> String {
    match url.query() {
        Some(query) => format!("{}?{}", url.path(), query),
        None => url.path().to_owned(),
    }
}

fn current_unix_timestamp() -> Result<i64, UmbraError> {
    let duration = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|_| UmbraError::auth("system clock is before unix epoch"))?;
    Ok(duration.as_secs() as i64)
}

async fn decode_envelope<T>(response: reqwest::Response) -> Result<T, UmbraError>
where
    T: DeserializeOwned,
{
    let status = response.status();
    let bytes = response.bytes().await?;
    let envelope: Envelope<T> = serde_json::from_slice(&bytes)?;
    if !status.is_success() || envelope.code != 0 {
        return Err(UmbraError::api(status, envelope.code, envelope.msg));
    }
    envelope
        .data
        .ok_or_else(|| UmbraError::invalid_input("response data is missing"))
}
