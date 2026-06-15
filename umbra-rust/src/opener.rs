use async_trait::async_trait;

use crate::UmbraError;

#[async_trait]
pub trait BrowserOpener: Send + Sync {
    async fn open_url(&self, url: &str) -> Result<(), UmbraError>;
}

pub struct SystemBrowserOpener;

#[async_trait]
impl BrowserOpener for SystemBrowserOpener {
    async fn open_url(&self, url: &str) -> Result<(), UmbraError> {
        #[cfg(target_os = "windows")]
        {
            tokio::process::Command::new("rundll32")
                .arg("url.dll,FileProtocolHandler")
                .arg(url)
                .spawn()?;
        }
        #[cfg(target_os = "macos")]
        {
            tokio::process::Command::new("open").arg(url).spawn()?;
        }
        #[cfg(all(unix, not(target_os = "macos")))]
        {
            tokio::process::Command::new("xdg-open").arg(url).spawn()?;
        }
        Ok(())
    }
}

pub struct NoopBrowserOpener;

#[async_trait]
impl BrowserOpener for NoopBrowserOpener {
    async fn open_url(&self, _url: &str) -> Result<(), UmbraError> {
        Ok(())
    }
}
