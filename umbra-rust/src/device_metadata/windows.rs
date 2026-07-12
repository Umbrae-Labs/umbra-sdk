use std::collections::BTreeMap;
use std::os::windows::process::CommandExt;

const CREATE_NO_WINDOW: u32 = 0x0800_0000;

use crate::{
    device_metadata::{
        load_or_create_windows_install_id, parse_reg_query_value, WindowsDeviceMetadataOptions,
        WindowsDeviceMetadataSource,
    },
    UmbraError,
};

pub(super) fn detect_source(
    options: &WindowsDeviceMetadataOptions,
) -> Result<WindowsDeviceMetadataSource, UmbraError> {
    let install_id = if let Some(install_id) = options
        .install_id
        .as_deref()
        .map(str::trim)
        .filter(|id| !id.is_empty())
    {
        Some(install_id.to_owned())
    } else if let Some(path) = options.install_id_path.as_deref() {
        Some(load_or_create_windows_install_id(path)?)
    } else {
        None
    };
    let machine_guid = if options.skip_machine_guid_hash {
        None
    } else {
        read_windows_registry_value(r"HKLM\SOFTWARE\Microsoft\Cryptography", "MachineGuid")
            .ok()
            .filter(|value| !value.trim().is_empty())
    };

    Ok(WindowsDeviceMetadataSource {
        hostname: std::env::var("COMPUTERNAME").unwrap_or_default(),
        arch: std::env::consts::ARCH.to_string(),
        registry: read_current_version_registry(),
        install_id,
        machine_guid,
    })
}

fn read_current_version_registry() -> BTreeMap<String, String> {
    let mut values = BTreeMap::new();
    for value in [
        "ProductName",
        "DisplayVersion",
        "CurrentBuildNumber",
        "UBR",
        "EditionID",
    ] {
        if let Ok(got) =
            read_windows_registry_value(r"HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion", value)
        {
            if !got.trim().is_empty() {
                values.insert(value.to_string(), got);
            }
        }
    }
    values
}

fn read_windows_registry_value(key: &str, value: &str) -> Result<String, UmbraError> {
    let output = std::process::Command::new("reg")
        .args(["query", key, "/v", value])
        .creation_flags(CREATE_NO_WINDOW)
        .output()?;
    Ok(parse_reg_query_value(
        &String::from_utf8_lossy(&output.stdout),
        value,
    ))
}
