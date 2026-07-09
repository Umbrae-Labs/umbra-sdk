mod api;
mod auth;
mod backup;
mod callback;
mod client;
mod config;
mod device;
mod device_metadata;
mod error;
mod opener;
mod store;
mod user;

pub use auth::{AuthCallback, AuthClient, Session, TokenSet};
pub use backup::{
    BackupAddress, BackupCategory, BackupClient, BackupListFilter, BackupRecord, BackupTarget,
    BatchConfirmResult, BatchConfirmResultItem, BatchItemError, BatchPresignResultItem,
    ConfirmUploadResult, DeleteResult, DownloadOptions, DownloadResult, NegotiateItem,
    NegotiateResult, PresignDownloadResult, PresignUploadInput, PresignUploadResult, UploadOptions,
    UploadResult,
};
pub use callback::{CallbackReceiver, LoopbackCallbackReceiver};
pub use client::UmbraClient;
pub use config::{DiscoveryMode, UmbraConfig, UmbraConfigBuilder};
pub use device::{
    canonical_string, parse_registration_token, request_body_hash, sign_canonical_string,
    ClientDevice, DeviceClient, DeviceCredential, DeviceMetadata, DeviceRegistrationInput,
    DeviceRegistrationResult,
};
pub use device_metadata::{
    detect_windows_device_metadata, load_or_create_windows_install_id, parse_reg_query_value,
    WindowsDeviceMetadataOptions,
};
pub use error::{ErrorKind, UmbraError};
pub use opener::{BrowserOpener, NoopBrowserOpener, SystemBrowserOpener};
pub use store::{
    DeviceCredentialStore, FileDeviceCredentialStore, FileTokenStore, MemoryDeviceCredentialStore,
    MemoryTokenStore, TokenStore,
};
pub use user::{QuotaInfo, UserClient, UserProfile};
