use std::{
    collections::BTreeMap,
    path::{Path, PathBuf},
};

use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine as _};
use rand::RngCore;
use serde_json::Value;
use sha2::{Digest, Sha256};

use crate::{device::DeviceMetadata, UmbraError};

#[cfg(windows)]
mod windows;

#[derive(Debug, Clone, Default)]
pub struct WindowsDeviceMetadataOptions {
    pub app_version: Option<String>,
    pub install_id: Option<String>,
    pub install_id_path: Option<PathBuf>,
    pub machine_guid_hash_salt: Option<String>,
    pub skip_machine_guid_hash: bool,
    pub metadata: BTreeMap<String, Value>,
}

#[derive(Debug, Clone, Default)]
pub struct WindowsDeviceMetadataSource {
    pub hostname: String,
    pub arch: String,
    pub registry: BTreeMap<String, String>,
    pub install_id: Option<String>,
    pub machine_guid: Option<String>,
}

pub fn detect_windows_device_metadata(
    options: WindowsDeviceMetadataOptions,
) -> Result<DeviceMetadata, UmbraError> {
    #[cfg(windows)]
    {
        return build_windows_device_metadata(windows::detect_source(&options)?, options);
    }
    #[cfg(not(windows))]
    {
        let _ = options;
        Err(UmbraError::invalid_input(
            "windows device metadata detection is only supported on windows",
        ))
    }
}

pub fn build_windows_device_metadata(
    source: WindowsDeviceMetadataSource,
    options: WindowsDeviceMetadataOptions,
) -> Result<DeviceMetadata, UmbraError> {
    let mut metadata = options.metadata;
    let install_id = options
        .install_id
        .as_deref()
        .map(str::trim)
        .filter(|id| !id.is_empty())
        .map(str::to_owned)
        .or_else(|| {
            source
                .install_id
                .as_deref()
                .map(str::trim)
                .filter(|id| !id.is_empty())
                .map(str::to_owned)
        });
    if let Some(install_id) = install_id {
        metadata.insert("install_id".to_string(), serde_json::json!(install_id));
    }
    if !options.skip_machine_guid_hash {
        if let Some(machine_guid) = source
            .machine_guid
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            metadata.insert(
                "machine_guid_hash".to_string(),
                serde_json::json!(hash_windows_machine_guid(
                    machine_guid,
                    options
                        .machine_guid_hash_salt
                        .as_deref()
                        .unwrap_or_default()
                )),
            );
        }
    }
    metadata.insert(
        "windows".to_string(),
        serde_json::json!({
            "product_name": source.registry.get("ProductName").cloned().unwrap_or_default(),
            "display_version": source.registry.get("DisplayVersion").cloned().unwrap_or_default(),
            "build": source.registry.get("CurrentBuildNumber").cloned().unwrap_or_default(),
            "ubr": source.registry.get("UBR").cloned().unwrap_or_default(),
            "edition_id": source.registry.get("EditionID").cloned().unwrap_or_default(),
        }),
    );

    Ok(DeviceMetadata {
        name: source.hostname.trim().to_owned(),
        platform: Some(format!("windows-{}", normalize_windows_arch(&source.arch))),
        os_version: Some(windows_os_version(&source.registry)),
        app_version: options
            .app_version
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .map(str::to_owned),
        metadata,
    })
}

pub fn load_or_create_windows_install_id(path: impl AsRef<Path>) -> Result<String, UmbraError> {
    let path = path.as_ref();
    match std::fs::read_to_string(path) {
        Ok(value) if !value.trim().is_empty() => return Ok(value.trim().to_owned()),
        Ok(_) => {}
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {}
        Err(err) => return Err(err.into()),
    }
    let install_id = new_install_id();
    if let Some(parent) = path.parent() {
        if !parent.as_os_str().is_empty() {
            std::fs::create_dir_all(parent)?;
        }
    }
    std::fs::write(path, format!("{install_id}\n"))?;
    Ok(install_id)
}

pub fn parse_reg_query_value(output: &str, value: &str) -> String {
    let wanted = value.to_lowercase();
    for line in output.lines().map(str::trim) {
        if !line.to_lowercase().starts_with(&wanted) {
            continue;
        }
        let fields: Vec<_> = line.split_whitespace().collect();
        if fields.len() < 3 {
            return String::new();
        }
        return fields[2..].join(" ");
    }
    String::new()
}

fn windows_os_version(values: &BTreeMap<String, String>) -> String {
    let mut version = values
        .get("ProductName")
        .map(String::as_str)
        .unwrap_or("Windows")
        .trim()
        .to_owned();
    if version.is_empty() {
        version = "Windows".to_string();
    }
    if let Some(display_version) = values.get("DisplayVersion").map(String::as_str) {
        if !display_version.trim().is_empty() {
            version.push(' ');
            version.push_str(display_version.trim());
        }
    }
    if let Some(build) = values.get("CurrentBuildNumber").map(String::as_str) {
        if !build.trim().is_empty() {
            version.push_str(" build ");
            version.push_str(build.trim());
            if let Some(ubr) = values.get("UBR").map(String::as_str) {
                if ubr.trim().parse::<u64>().is_ok() {
                    version.push('.');
                    version.push_str(ubr.trim());
                }
            }
        }
    }
    version.trim().to_owned()
}

fn hash_windows_machine_guid(machine_guid: &str, salt: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(salt.trim().as_bytes());
    hasher.update(machine_guid.trim().as_bytes());
    URL_SAFE_NO_PAD.encode(hasher.finalize())
}

fn normalize_windows_arch(arch: &str) -> &str {
    match arch {
        "x86_64" => "amd64",
        "x86" => "386",
        value => value,
    }
}

fn new_install_id() -> String {
    let mut bytes = [0_u8; 18];
    rand::thread_rng().fill_bytes(&mut bytes);
    URL_SAFE_NO_PAD.encode(bytes)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_reg_query_value() {
        let output = r#"
HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion
    ProductName    REG_SZ    Windows 11 Pro
"#;
        assert_eq!(
            parse_reg_query_value(output, "ProductName"),
            "Windows 11 Pro"
        );
    }

    #[test]
    fn builds_windows_device_metadata() {
        let mut registry = BTreeMap::new();
        registry.insert("ProductName".to_string(), "Windows 11 Pro".to_string());
        registry.insert("DisplayVersion".to_string(), "23H2".to_string());
        registry.insert("CurrentBuildNumber".to_string(), "22631".to_string());
        registry.insert("UBR".to_string(), "3593".to_string());
        registry.insert("EditionID".to_string(), "Professional".to_string());

        let metadata = build_windows_device_metadata(
            WindowsDeviceMetadataSource {
                hostname: "LunaBook".to_string(),
                arch: "x86_64".to_string(),
                registry,
                install_id: Some("install-123".to_string()),
                machine_guid: Some("machine-guid".to_string()),
            },
            WindowsDeviceMetadataOptions {
                app_version: Some("1.0.0".to_string()),
                machine_guid_hash_salt: Some("client-id".to_string()),
                ..Default::default()
            },
        )
        .unwrap();

        assert_eq!(metadata.name, "LunaBook");
        assert_eq!(metadata.platform.as_deref(), Some("windows-amd64"));
        assert_eq!(
            metadata.os_version.as_deref(),
            Some("Windows 11 Pro 23H2 build 22631.3593")
        );
        assert_eq!(metadata.app_version.as_deref(), Some("1.0.0"));
        assert_eq!(
            metadata.metadata.get("install_id"),
            Some(&serde_json::json!("install-123"))
        );
        assert!(metadata.metadata.contains_key("machine_guid_hash"));
    }

    #[test]
    fn persists_windows_install_id() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("install_id");
        let first = load_or_create_windows_install_id(&path).unwrap();
        let second = load_or_create_windows_install_id(&path).unwrap();
        assert_eq!(first, second);
        assert_eq!(std::fs::read_to_string(path).unwrap(), format!("{first}\n"));
    }
}
