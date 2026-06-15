use std::{collections::BTreeMap, sync::Arc};

use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine as _};
use hmac::{Hmac, Mac};
use rand::RngCore;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::{api::ApiClient, UmbraError};

pub(crate) const REGISTRATION_DEVICE_ID: &str = "registration";

type HmacSha256 = Hmac<Sha256>;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DeviceCredential {
    pub device_id: String,
    pub device_secret: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct DeviceMetadata {
    pub name: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub platform: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub os_version: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub app_version: Option<String>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub metadata: BTreeMap<String, serde_json::Value>,
}

#[derive(Debug, Clone, Serialize)]
pub struct DeviceRegistrationInput {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub credential_id: Option<String>,
    #[serde(skip_serializing)]
    pub credential_secret: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub registration_token: Option<String>,
    pub device: DeviceMetadata,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DeviceRegistrationResult {
    pub device: ClientDevice,
    pub device_secret: String,
    pub secret_once: bool,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ClientDevice {
    pub device_id: String,
    #[serde(default)]
    pub user_id: Option<u64>,
    #[serde(default)]
    pub tenant_id: Option<u64>,
    #[serde(default)]
    pub client_id: Option<String>,
    #[serde(default)]
    pub distribution_credential_key: Option<String>,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub platform: Option<String>,
    #[serde(default)]
    pub os_version: Option<String>,
    #[serde(default)]
    pub app_version: Option<String>,
    #[serde(default)]
    pub status: Option<i32>,
}

#[derive(Clone)]
pub struct DeviceClient {
    api: Arc<ApiClient>,
}

impl DeviceRegistrationInput {
    pub fn with_credential(
        credential_id: impl Into<String>,
        credential_secret: impl Into<String>,
        device: DeviceMetadata,
    ) -> Self {
        Self {
            credential_id: Some(credential_id.into()),
            credential_secret: Some(credential_secret.into()),
            registration_token: None,
            device,
        }
    }

    pub fn with_registration_token(
        registration_token: impl Into<String>,
        device: DeviceMetadata,
    ) -> Self {
        Self {
            credential_id: None,
            credential_secret: None,
            registration_token: Some(registration_token.into()),
            device,
        }
    }

    pub(crate) fn registration_secret(&self) -> Result<String, UmbraError> {
        let token_credential = match self
            .registration_token
            .as_deref()
            .map(str::trim)
            .filter(|token| !token.is_empty())
        {
            Some(token) => Some(parse_registration_token(token)?),
            None => None,
        };
        let credential_id = self
            .credential_id
            .as_deref()
            .map(str::trim)
            .filter(|id| !id.is_empty())
            .or_else(|| {
                token_credential
                    .as_ref()
                    .map(|(credential_id, _)| credential_id.as_str())
            })
            .ok_or_else(|| {
                UmbraError::invalid_input("credential_id or registration_token is required")
            })?;
        if let Some((token_credential_id, _)) = token_credential.as_ref() {
            if credential_id != token_credential_id {
                return Err(UmbraError::invalid_input(
                    "credential_id does not match registration_token",
                ));
            }
        }
        if let Some(secret) = self
            .credential_secret
            .as_deref()
            .map(str::trim)
            .filter(|secret| !secret.is_empty())
        {
            return Ok(secret.to_owned());
        }
        token_credential.map(|(_, secret)| secret).ok_or_else(|| {
            UmbraError::invalid_input(
                "credential_secret is required when registration_token is not provided",
            )
        })
    }
}

impl DeviceClient {
    pub(crate) fn new(api: Arc<ApiClient>) -> Self {
        Self { api }
    }

    pub async fn register(
        &self,
        input: DeviceRegistrationInput,
    ) -> Result<DeviceRegistrationResult, UmbraError> {
        let secret = input.registration_secret()?;
        let result: DeviceRegistrationResult = self
            .api
            .post_registration_signed("/client/devices/register", &input, &secret)
            .await?;
        self.api
            .save_device_credential(DeviceCredential {
                device_id: result.device.device_id.clone(),
                device_secret: result.device_secret.clone(),
            })
            .await?;
        Ok(result)
    }

    pub async fn ensure_registered(
        &self,
        input: DeviceRegistrationInput,
    ) -> Result<DeviceCredential, UmbraError> {
        if let Some(credential) = self.api.load_device_credential().await? {
            if !credential.device_id.is_empty() && !credential.device_secret.is_empty() {
                return Ok(credential);
            }
        }
        let result = self.register(input).await?;
        Ok(DeviceCredential {
            device_id: result.device.device_id,
            device_secret: result.device_secret,
        })
    }

    pub async fn rotate_secret(
        &self,
        device_id: Option<&str>,
    ) -> Result<DeviceRegistrationResult, UmbraError> {
        let target_device_id = match device_id.map(str::trim).filter(|id| !id.is_empty()) {
            Some(device_id) => device_id.to_owned(),
            None => self
                .api
                .load_device_credential()
                .await?
                .map(|credential| credential.device_id)
                .filter(|device_id| !device_id.is_empty())
                .ok_or_else(|| UmbraError::invalid_input("device_id is required"))?,
        };
        let path = format!("/user/devices/{target_device_id}/rotate-secret");
        let result: DeviceRegistrationResult = self.api.post(&path, &serde_json::json!({})).await?;
        self.api
            .save_device_credential(DeviceCredential {
                device_id: result.device.device_id.clone(),
                device_secret: result.device_secret.clone(),
            })
            .await?;
        Ok(result)
    }

    pub async fn load_credential(&self) -> Result<Option<DeviceCredential>, UmbraError> {
        self.api.load_device_credential().await
    }

    pub async fn save_credential(&self, credential: &DeviceCredential) -> Result<(), UmbraError> {
        self.api.save_device_credential(credential.clone()).await
    }

    pub async fn clear_credential(&self) -> Result<(), UmbraError> {
        self.api.clear_device_credential().await
    }
}

pub fn request_body_hash(body: &[u8]) -> String {
    URL_SAFE_NO_PAD.encode(Sha256::digest(body))
}

pub fn canonical_string(
    method: &str,
    path_with_query: &str,
    timestamp: i64,
    nonce: &str,
    body_hash: &str,
    device_id: &str,
) -> String {
    format!(
        "v1\n{}\n{}\n{}\n{}\n{}\n{}",
        method.to_uppercase(),
        path_with_query,
        timestamp,
        nonce,
        body_hash,
        device_id
    )
}

pub fn sign_canonical_string(secret: &str, canonical: &str) -> String {
    let mut mac =
        HmacSha256::new_from_slice(secret.as_bytes()).expect("HMAC accepts any key length");
    mac.update(canonical.as_bytes());
    URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes())
}

pub(crate) fn random_nonce() -> String {
    let mut bytes = [0_u8; 16];
    rand::thread_rng().fill_bytes(&mut bytes);
    hex::encode(bytes)
}

pub fn parse_registration_token(token: &str) -> Result<(String, String), UmbraError> {
    let Some(rest) = token.strip_prefix("umbra_reg_v1_") else {
        return Err(UmbraError::invalid_input("registration_token is invalid"));
    };
    let Some((credential_id, secret)) = rest.split_once('.') else {
        return Err(UmbraError::invalid_input("registration_token is invalid"));
    };
    if credential_id.is_empty() || secret.is_empty() || secret.contains('.') {
        return Err(UmbraError::invalid_input("registration_token is invalid"));
    }
    Ok((credential_id.to_owned(), secret.to_owned()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn signs_documented_test_vector() {
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

    #[test]
    fn parses_registration_token() {
        assert_eq!(
            parse_registration_token("umbra_reg_v1_ucd_xxx.secret-value").unwrap(),
            ("ucd_xxx".to_string(), "secret-value".to_string())
        );
        assert!(parse_registration_token("bad").is_err());
        assert!(parse_registration_token("umbra_reg_v1_ucd_xxx.secret.value").is_err());
    }
}
