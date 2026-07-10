use std::sync::Arc;

use serde::{de::DeserializeOwned, Deserialize, Serialize};
use serde_json::Value;

use crate::{api::ApiClient, UmbraError};

pub const SYNC_PROTOCOL_VERSION: u32 = 1;

#[derive(Clone)]
pub struct SyncClient {
    api: Arc<ApiClient>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SyncSpace {
    pub name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SyncRecordKey {
    pub namespace: String,
    pub collection: String,
    pub record_id: String,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum SyncOperation {
    Upsert,
    Delete,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SyncMutation {
    pub mutation_id: String,
    pub key: SyncRecordKey,
    pub schema_version: u32,
    pub base_version: u64,
    pub operation: SyncOperation,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub payload: Option<Value>,
}

impl SyncMutation {
    pub fn upsert<T: Serialize>(
        mutation_id: impl Into<String>,
        key: SyncRecordKey,
        schema_version: u32,
        base_version: u64,
        payload: T,
    ) -> Result<Self, UmbraError> {
        Ok(Self {
            mutation_id: mutation_id.into(),
            key,
            schema_version,
            base_version,
            operation: SyncOperation::Upsert,
            payload: Some(serde_json::to_value(payload)?),
        })
    }

    pub fn delete(
        mutation_id: impl Into<String>,
        key: SyncRecordKey,
        schema_version: u32,
        base_version: u64,
    ) -> Self {
        Self {
            mutation_id: mutation_id.into(),
            key,
            schema_version,
            base_version,
            operation: SyncOperation::Delete,
            payload: None,
        }
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct SyncExchangeInput {
    pub protocol_version: u32,
    pub space: SyncSpace,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub cursor: String,
    #[serde(default)]
    pub mutations: Vec<SyncMutation>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub pull_limit: Option<usize>,
}

impl SyncExchangeInput {
    pub fn new(space_name: impl Into<String>) -> Self {
        Self {
            protocol_version: SYNC_PROTOCOL_VERSION,
            space: SyncSpace {
                name: space_name.into(),
            },
            cursor: String::new(),
            mutations: Vec::new(),
            pull_limit: None,
        }
    }
}

#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
pub struct SyncAcceptedMutation {
    pub mutation_id: String,
    pub record_version: u64,
    pub cursor: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SyncChange {
    pub cursor: String,
    pub key: SyncRecordKey,
    pub schema_version: u32,
    pub record_version: u64,
    pub operation: SyncOperation,
    #[serde(default)]
    pub payload: Option<Value>,
    pub writer_device_id: String,
}

impl SyncChange {
    pub fn decode_payload<T: DeserializeOwned>(&self) -> Result<T, UmbraError> {
        let payload = self
            .payload
            .clone()
            .ok_or_else(|| UmbraError::invalid_input("sync change has no payload"))?;
        Ok(serde_json::from_value(payload)?)
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct SyncConflict {
    pub mutation_id: String,
    pub reason: String,
    #[serde(default)]
    pub current: Option<SyncChange>,
}

#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
pub struct SyncRejectedMutation {
    pub mutation_id: String,
    pub reason: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct SyncExchangeResult {
    pub accepted: Vec<SyncAcceptedMutation>,
    pub conflicts: Vec<SyncConflict>,
    pub rejected: Vec<SyncRejectedMutation>,
    pub changes: Vec<SyncChange>,
    pub next_cursor: String,
    pub has_more: bool,
    pub reset_required: bool,
    #[serde(default)]
    pub reason: Option<String>,
    #[serde(default)]
    pub snapshot_cursor: Option<String>,
}

#[derive(Debug, Clone)]
pub struct SyncSnapshotInput {
    pub protocol_version: u32,
    pub space_name: String,
    pub cursor: Option<String>,
    pub limit: Option<usize>,
}

impl SyncSnapshotInput {
    pub fn new(space_name: impl Into<String>) -> Self {
        Self {
            protocol_version: SYNC_PROTOCOL_VERSION,
            space_name: space_name.into(),
            cursor: None,
            limit: None,
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct SyncSnapshotPage {
    pub records: Vec<SyncChange>,
    #[serde(default)]
    pub next_cursor: Option<String>,
    pub exchange_cursor: String,
    pub has_more: bool,
}

impl SyncClient {
    pub(crate) fn new(api: Arc<ApiClient>) -> Self {
        Self { api }
    }

    pub async fn exchange(
        &self,
        mut input: SyncExchangeInput,
    ) -> Result<SyncExchangeResult, UmbraError> {
        if input.protocol_version == 0 {
            input.protocol_version = SYNC_PROTOCOL_VERSION;
        }
        self.api.post("/client/sync/exchange", &input).await
    }

    pub async fn snapshot(
        &self,
        mut input: SyncSnapshotInput,
    ) -> Result<SyncSnapshotPage, UmbraError> {
        if input.protocol_version == 0 {
            input.protocol_version = SYNC_PROTOCOL_VERSION;
        }
        let mut query = vec![
            ("protocol_version", input.protocol_version.to_string()),
            ("space", input.space_name),
        ];
        if let Some(cursor) = input.cursor.filter(|value| !value.is_empty()) {
            query.push(("cursor", cursor));
        }
        if let Some(limit) = input.limit {
            query.push(("limit", limit.to_string()));
        }
        self.api.get("/client/sync/snapshot", &query).await
    }
}
