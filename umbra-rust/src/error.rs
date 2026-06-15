use thiserror::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ErrorKind {
    InvalidParams,
    InvalidToken,
    Forbidden,
    QuotaInsufficient,
    FileNotFound,
    FileAlreadyExists,
    UploadNotFound,
    StorageUnavailable,
    Network,
    Timeout,
    Internal,
    Auth,
}

#[derive(Debug, Error)]
pub enum UmbraError {
    #[error("api error: {message}")]
    Api {
        status: Option<reqwest::StatusCode>,
        code: Option<i32>,
        kind: ErrorKind,
        message: String,
    },

    #[error("network error: {0}")]
    Network(#[from] reqwest::Error),

    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    #[error("json error: {0}")]
    Json(#[from] serde_json::Error),

    #[error("auth error: {0}")]
    Auth(String),

    #[error("invalid input: {0}")]
    InvalidInput(String),
}

impl UmbraError {
    pub fn invalid_input(message: impl Into<String>) -> Self {
        Self::InvalidInput(message.into())
    }

    pub fn auth(message: impl Into<String>) -> Self {
        Self::Auth(message.into())
    }

    pub fn api(status: reqwest::StatusCode, code: i32, message: impl Into<String>) -> Self {
        Self::Api {
            status: Some(status),
            code: Some(code),
            kind: kind_for_code(status, code),
            message: message.into(),
        }
    }

    pub fn storage(status: reqwest::StatusCode, message: impl Into<String>) -> Self {
        Self::Api {
            status: Some(status),
            code: None,
            kind: ErrorKind::StorageUnavailable,
            message: message.into(),
        }
    }

    pub fn is_invalid_token(&self) -> bool {
        matches!(
            self,
            Self::Api {
                kind: ErrorKind::InvalidToken,
                ..
            }
        )
    }
}

fn kind_for_code(status: reqwest::StatusCode, code: i32) -> ErrorKind {
    match code {
        1001 => ErrorKind::InvalidParams,
        1004 => ErrorKind::InvalidToken,
        1005 => ErrorKind::Forbidden,
        2001 => ErrorKind::QuotaInsufficient,
        2002 => ErrorKind::FileNotFound,
        2010 => ErrorKind::UploadNotFound,
        2005 => ErrorKind::StorageUnavailable,
        5000 => ErrorKind::Internal,
        _ if status == reqwest::StatusCode::UNAUTHORIZED => ErrorKind::InvalidToken,
        _ if status == reqwest::StatusCode::FORBIDDEN => ErrorKind::Forbidden,
        _ if status.is_server_error() => ErrorKind::Internal,
        _ => ErrorKind::InvalidParams,
    }
}
