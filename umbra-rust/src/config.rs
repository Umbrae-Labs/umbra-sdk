use std::{sync::Arc, time::Duration};

use url::Url;

use crate::{
    device::DeviceRegistrationInput,
    opener::{BrowserOpener, SystemBrowserOpener},
    store::{DeviceCredentialStore, MemoryDeviceCredentialStore, MemoryTokenStore, TokenStore},
    CallbackReceiver,
};

const DEFAULT_SCOPE: &str = "openid offline_access";
const DEFAULT_REFRESH_SKEW: Duration = Duration::from_secs(60);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DiscoveryMode {
    Disabled,
    Optional,
    Required,
}

#[derive(Clone)]
pub struct UmbraConfig {
    pub base_url: String,
    pub client_id: String,
    pub redirect_uri: String,
    pub scope: String,

    pub api_base_url: Option<String>,
    pub authorization_endpoint: Option<String>,
    pub token_endpoint: Option<String>,
    pub revocation_endpoint: Option<String>,
    pub discovery: DiscoveryMode,

    pub http_client: reqwest::Client,
    pub token_store: Arc<dyn TokenStore>,
    pub device_store: Arc<dyn DeviceCredentialStore>,
    pub device_registration: Option<DeviceRegistrationInput>,
    pub browser_opener: Arc<dyn BrowserOpener>,
    pub callback_receiver: Option<Arc<dyn CallbackReceiver>>,
    pub refresh_skew: Duration,
}

pub struct UmbraConfigBuilder {
    base_url: Option<String>,
    client_id: Option<String>,
    redirect_uri: Option<String>,
    scope: Option<String>,
    api_base_url: Option<String>,
    authorization_endpoint: Option<String>,
    token_endpoint: Option<String>,
    revocation_endpoint: Option<String>,
    discovery: DiscoveryMode,
    http_client: Option<reqwest::Client>,
    token_store: Option<Arc<dyn TokenStore>>,
    device_store: Option<Arc<dyn DeviceCredentialStore>>,
    device_registration: Option<DeviceRegistrationInput>,
    browser_opener: Option<Arc<dyn BrowserOpener>>,
    callback_receiver: Option<Arc<dyn CallbackReceiver>>,
    refresh_skew: Option<Duration>,
}

#[derive(Debug, Clone)]
pub(crate) struct Endpoints {
    pub api_base_url: String,
    pub authorization_endpoint: String,
    pub token_endpoint: String,
    pub revocation_endpoint: String,
}

impl UmbraConfig {
    pub fn builder() -> UmbraConfigBuilder {
        UmbraConfigBuilder::default()
    }

    pub(crate) fn endpoints(&self) -> Endpoints {
        Endpoints {
            api_base_url: self
                .api_base_url
                .clone()
                .unwrap_or_else(|| join_url(&self.base_url, "/api/v1")),
            authorization_endpoint: self
                .authorization_endpoint
                .clone()
                .unwrap_or_else(|| join_url(&self.base_url, "/oauth2/auth")),
            token_endpoint: self
                .token_endpoint
                .clone()
                .unwrap_or_else(|| join_url(&self.base_url, "/oauth2/token")),
            revocation_endpoint: self
                .revocation_endpoint
                .clone()
                .unwrap_or_else(|| join_url(&self.base_url, "/oauth2/revoke")),
        }
    }
}

impl Default for UmbraConfigBuilder {
    fn default() -> Self {
        Self {
            base_url: None,
            client_id: None,
            redirect_uri: None,
            scope: None,
            api_base_url: None,
            authorization_endpoint: None,
            token_endpoint: None,
            revocation_endpoint: None,
            discovery: DiscoveryMode::Disabled,
            http_client: None,
            token_store: None,
            device_store: None,
            device_registration: None,
            browser_opener: None,
            callback_receiver: None,
            refresh_skew: None,
        }
    }
}

impl UmbraConfigBuilder {
    pub fn base_url(mut self, value: impl Into<String>) -> Self {
        self.base_url = Some(value.into());
        self
    }

    pub fn client_id(mut self, value: impl Into<String>) -> Self {
        self.client_id = Some(value.into());
        self
    }

    pub fn redirect_uri(mut self, value: impl Into<String>) -> Self {
        self.redirect_uri = Some(value.into());
        self
    }

    pub fn scope(mut self, value: impl Into<String>) -> Self {
        self.scope = Some(value.into());
        self
    }

    pub fn api_base_url(mut self, value: impl Into<String>) -> Self {
        self.api_base_url = Some(trim_right_slash(value.into()));
        self
    }

    pub fn authorization_endpoint(mut self, value: impl Into<String>) -> Self {
        self.authorization_endpoint = Some(value.into());
        self
    }

    pub fn token_endpoint(mut self, value: impl Into<String>) -> Self {
        self.token_endpoint = Some(value.into());
        self
    }

    pub fn revocation_endpoint(mut self, value: impl Into<String>) -> Self {
        self.revocation_endpoint = Some(value.into());
        self
    }

    pub fn discovery(mut self, value: DiscoveryMode) -> Self {
        self.discovery = value;
        self
    }

    pub fn http_client(mut self, value: reqwest::Client) -> Self {
        self.http_client = Some(value);
        self
    }

    pub fn token_store<T>(mut self, value: T) -> Self
    where
        T: TokenStore + 'static,
    {
        self.token_store = Some(Arc::new(value));
        self
    }

    pub fn token_store_arc(mut self, value: Arc<dyn TokenStore>) -> Self {
        self.token_store = Some(value);
        self
    }

    pub fn device_store<T>(mut self, value: T) -> Self
    where
        T: DeviceCredentialStore + 'static,
    {
        self.device_store = Some(Arc::new(value));
        self
    }

    pub fn device_store_arc(mut self, value: Arc<dyn DeviceCredentialStore>) -> Self {
        self.device_store = Some(value);
        self
    }

    pub fn device_registration(mut self, value: DeviceRegistrationInput) -> Self {
        self.device_registration = Some(value);
        self
    }

    pub fn browser_opener<T>(mut self, value: T) -> Self
    where
        T: BrowserOpener + 'static,
    {
        self.browser_opener = Some(Arc::new(value));
        self
    }

    pub fn callback_receiver<T>(mut self, value: T) -> Self
    where
        T: CallbackReceiver + 'static,
    {
        self.callback_receiver = Some(Arc::new(value));
        self
    }

    pub fn refresh_skew(mut self, value: Duration) -> Self {
        self.refresh_skew = Some(value);
        self
    }

    pub fn build(self) -> Result<UmbraConfig, crate::UmbraError> {
        let base_url = trim_right_slash(
            self.base_url
                .ok_or_else(|| crate::UmbraError::invalid_input("base_url is required"))?,
        );
        parse_absolute_url(&base_url, "base_url")?;

        let client_id = self
            .client_id
            .map(|s| s.trim().to_owned())
            .filter(|s| !s.is_empty())
            .ok_or_else(|| crate::UmbraError::invalid_input("client_id is required"))?;

        let redirect_uri = self
            .redirect_uri
            .unwrap_or_else(|| "http://127.0.0.1:0/auth/callback".to_string())
            .trim()
            .to_owned();

        let config = UmbraConfig {
            base_url,
            client_id,
            redirect_uri,
            scope: self
                .scope
                .map(|s| s.trim().to_owned())
                .filter(|s| !s.is_empty())
                .unwrap_or_else(|| DEFAULT_SCOPE.to_string()),
            api_base_url: self.api_base_url,
            authorization_endpoint: self.authorization_endpoint,
            token_endpoint: self.token_endpoint,
            revocation_endpoint: self.revocation_endpoint,
            discovery: self.discovery,
            http_client: self.http_client.unwrap_or_default(),
            token_store: self
                .token_store
                .unwrap_or_else(|| Arc::new(MemoryTokenStore::default())),
            device_store: self
                .device_store
                .unwrap_or_else(|| Arc::new(MemoryDeviceCredentialStore::default())),
            device_registration: self.device_registration,
            browser_opener: self
                .browser_opener
                .unwrap_or_else(|| Arc::new(SystemBrowserOpener)),
            callback_receiver: self.callback_receiver,
            refresh_skew: self.refresh_skew.unwrap_or(DEFAULT_REFRESH_SKEW),
        };

        let endpoints = config.endpoints();
        parse_absolute_url(&endpoints.api_base_url, "api_base_url")?;
        parse_absolute_url(&endpoints.authorization_endpoint, "authorization_endpoint")?;
        parse_absolute_url(&endpoints.token_endpoint, "token_endpoint")?;
        parse_absolute_url(&endpoints.revocation_endpoint, "revocation_endpoint")?;

        Ok(config)
    }
}

pub(crate) fn join_url(base: &str, path: &str) -> String {
    format!(
        "{}/{}",
        base.trim_end_matches('/'),
        path.trim_start_matches('/')
    )
}

fn trim_right_slash(value: String) -> String {
    value.trim().trim_end_matches('/').to_owned()
}

fn parse_absolute_url(raw: &str, name: &str) -> Result<Url, crate::UmbraError> {
    let parsed = Url::parse(raw)
        .map_err(|_| crate::UmbraError::invalid_input(format!("{name} must be an absolute URL")))?;
    if parsed.scheme().is_empty() || parsed.host_str().is_none() {
        return Err(crate::UmbraError::invalid_input(format!(
            "{name} must be an absolute URL"
        )));
    }
    Ok(parsed)
}
