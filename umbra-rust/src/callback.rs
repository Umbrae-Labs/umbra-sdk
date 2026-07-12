use std::{net::SocketAddr, sync::Arc, time::Duration};

use async_trait::async_trait;
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream},
    sync::Mutex,
};
use url::Url;

use crate::{AuthCallback, UmbraError};

#[async_trait]
pub trait CallbackReceiver: Send + Sync {
    async fn receive(&self, expected_state: &str) -> Result<AuthCallback, UmbraError>;
}

pub struct LoopbackCallbackReceiver {
    redirect_uri: String,
    listener: Arc<Mutex<Option<TcpListener>>>,
}

impl LoopbackCallbackReceiver {
    pub async fn bind(redirect_uri: &str) -> Result<Self, UmbraError> {
        let mut parsed = Url::parse(redirect_uri)
            .map_err(|_| UmbraError::invalid_input("redirect_uri must be a valid URL"))?;
        if parsed.scheme() != "http" || parsed.host_str() != Some("127.0.0.1") {
            return Err(UmbraError::invalid_input(
                "redirect_uri must be http://127.0.0.1:<port>/<path>",
            ));
        }
        let port = parsed.port().unwrap_or(0);
        let listener = TcpListener::bind(("127.0.0.1", port)).await?;
        let actual: SocketAddr = listener.local_addr()?;
        parsed
            .set_port(Some(actual.port()))
            .map_err(|_| UmbraError::invalid_input("invalid redirect_uri port"))?;
        parsed.set_query(None);
        parsed.set_fragment(None);
        Ok(Self {
            redirect_uri: parsed.to_string(),
            listener: Arc::new(Mutex::new(Some(listener))),
        })
    }

    pub fn redirect_uri(&self) -> &str {
        &self.redirect_uri
    }
}

#[async_trait]
impl CallbackReceiver for LoopbackCallbackReceiver {
    async fn receive(&self, expected_state: &str) -> Result<AuthCallback, UmbraError> {
        let listener = self
            .listener
            .lock()
            .await
            .take()
            .ok_or_else(|| UmbraError::auth("callback receiver already used"))?;
        let (stream, _) = listener.accept().await?;
        let callback = handle_stream(stream).await?;
        if callback.state != expected_state {
            return Err(UmbraError::auth("authorization state mismatch"));
        }
        Ok(callback)
    }
}

async fn handle_stream(mut stream: TcpStream) -> Result<AuthCallback, UmbraError> {
    let mut buf = vec![0_u8; 8192];
    let n = tokio::time::timeout(Duration::from_secs(5), stream.read(&mut buf))
        .await
        .map_err(|_| UmbraError::auth("callback read timed out"))??;
    let request = String::from_utf8_lossy(&buf[..n]);
    let first_line = request
        .lines()
        .next()
        .ok_or_else(|| UmbraError::auth("callback request is empty"))?;
    let target = first_line
        .split_whitespace()
        .nth(1)
        .ok_or_else(|| UmbraError::auth("callback request target missing"))?;
    let parsed = Url::parse(&format!("http://127.0.0.1{target}"))
        .map_err(|_| UmbraError::auth("callback request target invalid"))?;
    let mut callback = AuthCallback::default();
    for (key, value) in parsed.query_pairs() {
        match key.as_ref() {
            "code" => callback.code = value.into_owned(),
            "state" => callback.state = value.into_owned(),
            "error" => callback.error = Some(value.into_owned()),
            _ => {}
        }
    }
    stream
        .write_all(b"HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
        .await?;
    Ok(callback)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn loopback_callback_returns_no_content() {
        let listener = TcpListener::bind(("127.0.0.1", 0)).await.unwrap();
        let address = listener.local_addr().unwrap();
        let callback_task = tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            handle_stream(stream).await.unwrap()
        });

        let mut stream = TcpStream::connect(address).await.unwrap();
        stream
            .write_all(b"GET /auth/callback?code=test-code&state=test-state HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")
            .await
            .unwrap();
        let mut response = Vec::new();
        stream.read_to_end(&mut response).await.unwrap();

        assert_eq!(
            String::from_utf8(response).unwrap(),
            "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"
        );
        let callback = callback_task.await.unwrap();
        assert_eq!(callback.code, "test-code");
        assert_eq!(callback.state, "test-state");
    }
}
