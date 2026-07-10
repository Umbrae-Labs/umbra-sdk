# umbra-go

Go SDK for Umbra desktop clients.

Status: MVP SDK implementation.

## Install

```bash
go get github.com/Umbrae-Labs/umbra-sdk/umbra-go@latest
```

## Recommended Desktop Usage

The desktop client receives a setup package from Umbra admin/client-access. Keep
its `registration_token` with the app installer or managed configuration, then
let the SDK register the local device after OAuth login.

```go
package main

import (
	"context"

	umbra "github.com/Umbrae-Labs/umbra-sdk/umbra-go"
)

func main() {
	ctx := context.Background()

	device, err := umbra.DetectWindowsDeviceMetadata(umbra.WindowsDeviceMetadataOptions{
		AppVersion:          "1.0.0",
		InstallIDPath:       "device-install-id",
		MachineGUIDHashSalt: "lunabox-desktop",
	})
	if err != nil {
		panic(err)
	}

	client, err := umbra.New(umbra.Config{
		BaseURL:     "https://umbra.example.com",
		ClientID:    "lunabox-desktop",
		RedirectURI: "http://127.0.0.1:1420/auth/callback",

		TokenStore:    umbra.NewFileTokenStore("tokens.json"),
		DeviceStore:   umbra.NewFileDeviceStore("device.json"),
		BrowserOpener: umbra.SystemBrowserOpener{},

		DeviceRegistration: &umbra.DeviceRegistrationOptions{
			RegistrationToken: "umbra_reg_v1_ucd_xxx.secret_xxx",
			Device:            device,
		},
	})
	if err != nil {
		panic(err)
	}

	// OAuth login. When DeviceRegistration is configured, this also registers
	// the device if device.json has no usable device_id + device_secret yet.
	if _, err := client.Login(ctx); err != nil {
		panic(err)
	}

	quota, err := client.User.Quota(ctx)
	if err != nil {
		panic(err)
	}
	_ = quota
}
```

`DetectWindowsDeviceMetadata` is Windows-only. It collects host name, runtime
architecture, Windows version registry values, a stable random `install_id`,
and a hashed `MachineGuid`. Metadata is for display and audit only; request
trust comes from the server-issued `device_id + device_secret`.

Device registration only accepts metadata returned by SDK detection helpers.
Manually constructed `DeviceMetadata` values are rejected by `Register` and
`EnsureRegistered`.

## Manual Device Registration

If you do not want root `client.Login` to auto-register the device, keep using
`client.Auth.Login` and call device registration explicitly.

```go
if _, err := client.Auth.Login(ctx); err != nil {
	panic(err)
}

device, err := umbra.DetectWindowsDeviceMetadata(umbra.WindowsDeviceMetadataOptions{
	AppVersion: "1.0.0",
})
if err != nil {
	panic(err)
}

_, err := client.Devices.EnsureRegistered(ctx, umbra.DeviceRegistrationOptions{
	RegistrationToken: "umbra_reg_v1_ucd_xxx.secret_xxx",
	Device:            device,
})
```

You can also register with separate credential fields:

```go
_, err := client.Devices.Register(ctx, umbra.DeviceRegistrationOptions{
	CredentialID:     "ucd_xxx",
	CredentialSecret: "credential-secret",
	Device:           device,
})
```

## Backup Upload

Backup APIs under `/client/backup/*` are device-signature protected. Once
`DeviceStore` has credentials, the SDK signs these requests automatically.

```go
result, err := client.Backup.UploadFile(
	ctx,
	umbra.GameBackup("mc", "2026-05-10T20-00-00"),
	"world.zip",
	umbra.UploadOptions{
		ComputeHash:     true,
		NegotiateByHash: true,
		Progress: func(done, total uint64) {},
	},
)
_ = result
```

`UploadFile` performs:

1. optional SHA-256 calculation
2. optional negotiate-by-hash
3. signed Umbra presign request
4. object storage PUT
5. signed Umbra confirm request

## Structured Sync

Structured JSON data uses the independent, device-signed sync client. The SDK
serializes the protocol and returns conflicts as normal result data; the
application remains responsible for its local database transaction and merge
policy.

```go
key := umbra.SyncRecordKey{
	Namespace: "lunabox.library",
	Collection: "games",
	RecordID: "game-1",
}
mutation, err := umbra.NewUpsertMutation("mutation-1", key, 1, 0, map[string]any{
	"name": "Example Game",
})
if err != nil {
	panic(err)
}
result, err := client.Sync.Exchange(ctx, umbra.SyncExchangeInput{
	Space:     umbra.SyncSpace{Name: "library"},
	Mutations: []umbra.SyncMutation{mutation},
})
```

Persist `result.NextCursor` only after applying `result.Changes` in a successful
local transaction. Use `client.Sync.Snapshot` when bootstrap is required. The
legacy object-backup `sync` category has been removed; object backups support
only `db`, `full`, `game`, and `asset`.

## Device Secret Rotation

```go
rotated, err := client.Devices.RotateSecret(ctx, "")
```

Passing an empty device ID uses the locally stored device. On success, the SDK
stores the new `device_secret` in `DeviceStore`.

## Same-Origin Defaults

Given:

```text
BaseURL = https://umbra.example.com
```

The SDK derives:

```text
APIBaseURL            = https://umbra.example.com/api/v1
AuthorizationEndpoint = https://umbra.example.com/oauth2/auth
TokenEndpoint         = https://umbra.example.com/oauth2/token
RevocationEndpoint    = https://umbra.example.com/oauth2/revoke
```

Advanced deployments can override any endpoint in `Config`.

## Notes

- `FileTokenStore` and `FileDeviceStore` are for development and examples. Production desktop apps should use OS keychain storage.
- `registration_token` and `device_secret` are sensitive. Do not store them in ordinary user-editable config.
- Loopback redirects should use `127.0.0.1`, not `localhost`, when using random ports.
- Public releases are fetched from `github.com/Umbrae-Labs/umbra-sdk/umbra-go`.
