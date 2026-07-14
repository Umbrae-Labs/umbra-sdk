use std::sync::Arc;

use crate::{
    api::ApiClient,
    device::{DeviceClient, DeviceRegistrationInput},
    AuthClient, BackupClient, Session, SyncClient, UmbraConfig, UmbraConfigBuilder, UmbraError,
    UserClient,
};

#[derive(Clone)]
pub struct UmbraClient {
    auth: AuthClient,
    user: UserClient,
    backups: BackupClient,
    devices: DeviceClient,
    sync: SyncClient,
    device_registration: Option<DeviceRegistrationInput>,
}

impl UmbraClient {
    pub fn builder() -> UmbraConfigBuilder {
        UmbraConfig::builder()
    }

    pub fn new(config: UmbraConfig) -> Result<Self, UmbraError> {
        let endpoints = config.endpoints();
        let auth = AuthClient::new(config.clone(), endpoints.clone());
        let api = Arc::new(ApiClient::new(
            config.http_client.clone(),
            endpoints.api_base_url,
            auth.clone(),
            config.device_store.clone(),
        ));
        let user = UserClient::new(api.clone());
        let backups = BackupClient::new(api.clone(), config.http_client.clone());
        let devices = DeviceClient::new(api.clone());
        let sync = SyncClient::new(api);
        let device_registration = config.device_registration.clone();
        Ok(Self {
            auth,
            user,
            backups,
            devices,
            sync,
            device_registration,
        })
    }

    pub fn auth(&self) -> &AuthClient {
        &self.auth
    }

    pub fn user(&self) -> &UserClient {
        &self.user
    }

    pub fn backups(&self) -> &BackupClient {
        &self.backups
    }

    pub fn devices(&self) -> &DeviceClient {
        &self.devices
    }

    pub fn sync(&self) -> &SyncClient {
        &self.sync
    }

    pub async fn login(&self) -> Result<Session, UmbraError> {
        let session = self.auth.login().await?;
        if let Some(registration) = self.device_registration.clone() {
            self.devices.ensure_registered(registration).await?;
        }
        Ok(session)
    }

    pub async fn logout(&self) -> Result<(), UmbraError> {
        self.auth.logout().await
    }
}
