use std::sync::Arc;

use serde::Deserialize;

use crate::{api::ApiClient, UmbraError};

#[derive(Clone)]
pub struct UserClient {
    api: Arc<ApiClient>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct QuotaInfo {
    pub quota_bytes: u64,
    pub used_bytes: u64,
    pub available_bytes: u64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UserProfile {
    pub id: u64,
    pub username: String,
    pub quota_bytes: u64,
    pub used_bytes: u64,
    pub available_bytes: u64,
    #[serde(default)]
    pub storage_end_id: Option<u64>,
}

impl UserClient {
    pub(crate) fn new(api: Arc<ApiClient>) -> Self {
        Self { api }
    }

    pub async fn quota(&self) -> Result<QuotaInfo, UmbraError> {
        self.api.get("/user/quota", &[]).await
    }

    pub async fn profile(&self) -> Result<UserProfile, UmbraError> {
        self.api.get("/user/profile", &[]).await
    }
}
