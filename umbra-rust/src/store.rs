use std::{
    path::{Path, PathBuf},
    sync::Arc,
};

use async_trait::async_trait;
use tokio::sync::RwLock;

use crate::{device::DeviceCredential, TokenSet, UmbraError};

#[async_trait]
pub trait TokenStore: Send + Sync {
    async fn load(&self) -> Result<Option<TokenSet>, UmbraError>;
    async fn save(&self, token: &TokenSet) -> Result<(), UmbraError>;
    async fn clear(&self) -> Result<(), UmbraError>;
}

#[async_trait]
pub trait DeviceCredentialStore: Send + Sync {
    async fn load(&self) -> Result<Option<DeviceCredential>, UmbraError>;
    async fn save(&self, credential: &DeviceCredential) -> Result<(), UmbraError>;
    async fn clear(&self) -> Result<(), UmbraError>;
}

#[derive(Default)]
pub struct MemoryTokenStore {
    token: RwLock<Option<TokenSet>>,
}

#[derive(Default)]
pub struct MemoryDeviceCredentialStore {
    credential: RwLock<Option<DeviceCredential>>,
}

#[async_trait]
impl TokenStore for MemoryTokenStore {
    async fn load(&self) -> Result<Option<TokenSet>, UmbraError> {
        Ok(self.token.read().await.clone())
    }

    async fn save(&self, token: &TokenSet) -> Result<(), UmbraError> {
        *self.token.write().await = Some(token.clone());
        Ok(())
    }

    async fn clear(&self) -> Result<(), UmbraError> {
        *self.token.write().await = None;
        Ok(())
    }
}

#[async_trait]
impl DeviceCredentialStore for MemoryDeviceCredentialStore {
    async fn load(&self) -> Result<Option<DeviceCredential>, UmbraError> {
        Ok(self.credential.read().await.clone())
    }

    async fn save(&self, credential: &DeviceCredential) -> Result<(), UmbraError> {
        *self.credential.write().await = Some(credential.clone());
        Ok(())
    }

    async fn clear(&self) -> Result<(), UmbraError> {
        *self.credential.write().await = None;
        Ok(())
    }
}

#[derive(Clone)]
pub struct FileTokenStore {
    path: Arc<PathBuf>,
}

impl FileTokenStore {
    pub fn new(path: impl Into<PathBuf>) -> Self {
        Self {
            path: Arc::new(path.into()),
        }
    }

    pub fn path(&self) -> &Path {
        &self.path
    }
}

#[derive(Clone)]
pub struct FileDeviceCredentialStore {
    path: Arc<PathBuf>,
}

impl FileDeviceCredentialStore {
    pub fn new(path: impl Into<PathBuf>) -> Self {
        Self {
            path: Arc::new(path.into()),
        }
    }

    pub fn path(&self) -> &Path {
        &self.path
    }
}

#[async_trait]
impl TokenStore for FileTokenStore {
    async fn load(&self) -> Result<Option<TokenSet>, UmbraError> {
        match tokio::fs::read(&*self.path).await {
            Ok(data) if data.is_empty() => Ok(None),
            Ok(data) => Ok(Some(serde_json::from_slice(&data)?)),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(None),
            Err(err) => Err(err.into()),
        }
    }

    async fn save(&self, token: &TokenSet) -> Result<(), UmbraError> {
        if let Some(parent) = self.path.parent() {
            if !parent.as_os_str().is_empty() {
                tokio::fs::create_dir_all(parent).await?;
            }
        }
        let data = serde_json::to_vec_pretty(token)?;
        tokio::fs::write(&*self.path, data).await?;
        Ok(())
    }

    async fn clear(&self) -> Result<(), UmbraError> {
        match tokio::fs::remove_file(&*self.path).await {
            Ok(()) => Ok(()),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(err) => Err(err.into()),
        }
    }
}

#[async_trait]
impl DeviceCredentialStore for FileDeviceCredentialStore {
    async fn load(&self) -> Result<Option<DeviceCredential>, UmbraError> {
        match tokio::fs::read(&*self.path).await {
            Ok(data) if data.is_empty() => Ok(None),
            Ok(data) => Ok(Some(serde_json::from_slice(&data)?)),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(None),
            Err(err) => Err(err.into()),
        }
    }

    async fn save(&self, credential: &DeviceCredential) -> Result<(), UmbraError> {
        if let Some(parent) = self.path.parent() {
            if !parent.as_os_str().is_empty() {
                tokio::fs::create_dir_all(parent).await?;
            }
        }
        let data = serde_json::to_vec_pretty(credential)?;
        tokio::fs::write(&*self.path, data).await?;
        Ok(())
    }

    async fn clear(&self) -> Result<(), UmbraError> {
        match tokio::fs::remove_file(&*self.path).await {
            Ok(()) => Ok(()),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(err) => Err(err.into()),
        }
    }
}
