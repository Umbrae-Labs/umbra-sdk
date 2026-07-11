# umbra-sdk

Rust SDK for Umbra desktop clients.

Status: MVP SDK implementation.

Package name: `umbra-sdk`

## Recommended Desktop Usage

The desktop client receives a setup package from Umbra admin/client-access. Keep
its `registration_token` with the app installer or managed configuration, then
let the SDK register the local device after OAuth login.

```rust
use umbra_sdk::{
    detect_windows_device_metadata, BackupAddress, DeviceRegistrationInput,
    FileDeviceCredentialStore, FileTokenStore, UmbraClient, UploadOptions,
    WindowsDeviceMetadataOptions,
};

#[tokio::main]
async fn main() -> Result<(), umbra_sdk::UmbraError> {
    let device = detect_windows_device_metadata(WindowsDeviceMetadataOptions {
        app_version: Some("1.0.0".to_string()),
        install_id_path: Some("device-install-id".into()),
        machine_guid_hash_salt: Some("lunabox-desktop".to_string()),
        ..Default::default()
    })?;

    let config = UmbraClient::builder()
        .base_url("https://umbra.example.com")
        .client_id("lunabox-desktop")
        .redirect_uri("http://127.0.0.1:0/auth/callback")
        .token_store(FileTokenStore::new("tokens.json"))
        .device_store(FileDeviceCredentialStore::new("device.json"))
        .device_registration(DeviceRegistrationInput::with_registration_token(
            "umbra_reg_v1_ucd_xxx.secret_xxx",
            device,
        ))
        .build()?;

    let client = UmbraClient::new(config)?;

    // OAuth login. When device_registration is configured, this also registers
    // the device if device.json has no usable device_id + device_secret yet.
    client.login().await?;

    let quota = client.user().quota().await?;
    println!("available: {}", quota.available_bytes);

    client
        .backups()
        .upload_file(
            BackupAddress::game("mc", "2026-05-10T20-00-00"),
            "world.zip",
            UploadOptions {
                compute_hash: true,
                negotiate_by_hash: true,
                ..Default::default()
            },
        )
        .await?;

    Ok(())
}
```

`detect_windows_device_metadata` is Windows-only. It collects host name,
runtime architecture, Windows version registry values, a stable random
`install_id`, and a hashed `MachineGuid`. Metadata is for display and audit
only; request trust comes from the server-issued `device_id + device_secret`.
Device registration only accepts metadata returned by SDK detection helpers.
Manually constructed `DeviceMetadata` values are rejected by `register` and
`ensure_registered`.

## Manual Device Registration

If you do not want root `client.login()` to auto-register the device, keep using
`client.auth().login()` and call device registration explicitly.

```rust
client.auth().login().await?;

client
    .devices()
    .ensure_registered(DeviceRegistrationInput::with_registration_token(
        "umbra_reg_v1_ucd_xxx.secret_xxx",
        device,
    ))
    .await?;
```

You can also use separate credential fields:

```rust
client
    .devices()
    .register(DeviceRegistrationInput::with_credential(
        "ucd_xxx",
        "credential-secret",
        device,
    ))
    .await?;
```

## Signed Backup Requests

Backup APIs under `/client/backup/*` are device-signature protected. Once
`DeviceCredentialStore` has credentials, the SDK signs these requests
automatically.

```rust
let presign = client
    .backups()
    .presign_upload(umbra_sdk::PresignUploadInput {
        address: BackupAddress::game("mc", "v1"),
        file_size: 1024,
        content_type: "application/zip".to_string(),
        content_hash: None,
    })
    .await?;
```

`upload_file` also uses signed backup API calls internally.

## Structured Sync

Structured JSON records use the independent `client.sync()` interface. The SDK
signs exchange and snapshot requests and returns conflicts as normal result
data; the application owns local database transactions and merge policy.

```rust
use serde_json::json;
use umbra_sdk::{SyncExchangeInput, SyncMutation, SyncRecordKey};

let key = SyncRecordKey {
    namespace: "lunabox.library".to_string(),
    collection: "games".to_string(),
    record_id: "game-1".to_string(),
};
let mut input = SyncExchangeInput::new("library");
input.mutations.push(SyncMutation::upsert(
    "mutation-1",
    key,
    1,
    0,
    json!({"name": "Example Game"}),
)?);
let result = client.sync().exchange(input).await?;
```

Read pending mutations from the local outbox and push multiple items into
`input.mutations` for one `exchange` call instead of calling the SDK once per
record. One request accepts at most 500 mutations and 4 MiB of JSON. The Rust
SDK does not split oversized input automatically, so a client-side target of
about 3.5 MiB leaves room for the request envelope.

Persist `result.next_cursor` only after applying `result.changes` in a
successful local transaction together with mutation outcomes. Reuse the
original mutation ID when retrying. For an initial upload into an empty space,
create mutations use base version `0`; if the remote space may contain data,
call `client.sync().snapshot(...)` before uploading. While `result.has_more` is
true, continue pulling with an empty `mutations` vector. The legacy object-backup
`sync` category has been removed; object backups support only `db`, `full`,
`game`, and `asset`.

## Device Secret Rotation

```rust
client.devices().rotate_secret(None).await?;
```

Passing `None` uses the locally stored device. On success, the SDK stores the
new `device_secret` in `DeviceCredentialStore`.

## Same-Origin Defaults

Given:

```text
base_url = https://umbra.example.com
```

The SDK derives:

```text
api_base_url           = https://umbra.example.com/api/v1
authorization_endpoint = https://umbra.example.com/oauth2/auth
token_endpoint         = https://umbra.example.com/oauth2/token
revocation_endpoint    = https://umbra.example.com/oauth2/revoke
```

Advanced deployments can override any endpoint in the builder.

## Notes

- `FileTokenStore` and `FileDeviceCredentialStore` are for development and examples. Production apps should use OS keychain-backed stores.
- `registration_token` and `device_secret` are sensitive. Do not store them in ordinary user-editable config.
- Loopback redirects should use `127.0.0.1`, not `localhost`, when using dynamic ports.
