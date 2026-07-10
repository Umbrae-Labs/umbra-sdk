# @umbrae-labs/umbra-sdk

TypeScript SDK for Umbra desktop clients.

Status: MVP SDK implementation.

Package name: `@umbrae-labs/umbra-sdk`

## Install

```bash
pnpm add @umbrae-labs/umbra-sdk
```

## Entries

- `@umbrae-labs/umbra-sdk`: core SDK. Uses Web APIs: `fetch`, Web Crypto, and app-provided storage.
- `@umbrae-labs/umbra-sdk/node`: Node helpers for token files, loopback callback, browser opening, file upload/download.
- `@umbrae-labs/umbra-sdk/electron`: Electron main-process helpers. Re-exports `node` helpers and adds Windows device metadata detection.

## Recommended Electron Usage

Run Windows metadata detection in the Electron main process, then configure
device registration. `client.login()` performs OAuth login and registers the
device when no stored `device_id + device_secret` exists.

```ts
import { gameBackup, UmbraClient } from '@umbrae-labs/umbra-sdk'
import {
  detectWindowsDeviceMetadata,
  FileDeviceCredentialStore,
  FileTokenStore,
  LoopbackCallbackReceiver,
  SystemBrowserOpener,
  uploadFile,
} from '@umbrae-labs/umbra-sdk/electron'

const device = await detectWindowsDeviceMetadata({
  appVersion: '1.0.0',
  installIdPath: 'device-install-id',
  machineGuidHashSalt: 'lunabox-desktop',
})

const client = new UmbraClient({
  baseUrl: 'https://umbra.example.com',
  clientId: 'lunabox-desktop',
  redirectUri: 'http://127.0.0.1:0/auth/callback',

  tokenStore: new FileTokenStore('tokens.json'),
  deviceStore: new FileDeviceCredentialStore('device.json'),
  browserOpener: new SystemBrowserOpener(),
  callbackReceiver: new LoopbackCallbackReceiver(),

  deviceRegistration: {
    registrationToken: 'umbra_reg_v1_ucd_xxx.secret_xxx',
    device,
  },
})

await client.login()

await uploadFile(
  client.backups,
  gameBackup('mc', '2026-05-10T20-00-00'),
  'world.zip',
  {
    computeHash: true,
    negotiateByHash: true,
  },
)
```

`detectWindowsDeviceMetadata` reads Windows version registry values and hashes
`MachineGuid` before placing it in metadata. Metadata is for display and audit
only; request trust comes from the server-issued `device_id + device_secret`.
Device registration only accepts metadata returned by SDK detection helpers.
Plain object literals are rejected by `register` and `ensureRegistered`.

## Core Usage

The core entry can be used when the app supplies storage and does not need
Node/Electron file helpers. For device registration, pass metadata collected by
the platform helper, such as `detectWindowsDeviceMetadata` from the Electron
entry.

```ts
import { MemoryDeviceCredentialStore, MemoryTokenStore, UmbraClient } from '@umbrae-labs/umbra-sdk'
import { detectWindowsDeviceMetadata } from '@umbrae-labs/umbra-sdk/electron'

const device = await detectWindowsDeviceMetadata({ appVersion: '1.0.0' })

const client = new UmbraClient({
  baseUrl: 'https://umbra.example.com',
  clientId: 'lunabox-desktop',
  redirectUri: 'http://127.0.0.1:0/auth/callback',
  tokenStore: new MemoryTokenStore(),
  deviceStore: new MemoryDeviceCredentialStore(),
  deviceRegistration: {
    registrationToken: 'umbra_reg_v1_ucd_xxx.secret_xxx',
    device,
  },
})

await client.login()

const quota = await client.user.quota()
```

## Manual Device Registration

If you need separate control over OAuth login and device registration:

```ts
await client.auth.login()

const device = await detectWindowsDeviceMetadata({ appVersion: '1.0.0' })

await client.devices.ensureRegistered({
  registrationToken: 'umbra_reg_v1_ucd_xxx.secret_xxx',
  device,
})
```

You can also use separate credential fields:

```ts
await client.devices.register({
  credentialId: 'ucd_xxx',
  credentialSecret: 'credential-secret',
  device,
})
```

## Signed Backup Requests

Backup APIs under `/client/backup/*` are device-signature protected. Once
`deviceStore` has credentials, the SDK signs these requests automatically.

```ts
await client.backups.presignUpload({
  address: gameBackup('mc', 'v1'),
  fileSize: 1024,
  contentType: 'application/zip',
})
```

Node/Electron file helpers also use the signed backup API automatically:

```ts
await uploadFile(client.backups, gameBackup('mc', 'v1'), 'world.zip')
```

## Structured Sync

Structured JSON records use `client.sync`, independently from object backups.
Exchange and snapshot requests are signed automatically. Conflicts and rejected
mutations are returned as result arrays; the application owns local database
transactions and conflict resolution.

```ts
import { newUpsertMutation } from '@umbrae-labs/umbra-sdk'

const mutation = newUpsertMutation(
  'mutation-1',
  { namespace: 'lunabox.library', collection: 'games', record_id: 'game-1' },
  1,
  0,
  { name: 'Example Game' },
)

const result = await client.sync.exchange({
  space: { name: 'library' },
  mutations: [mutation],
})
```

Persist `result.next_cursor` only after applying `result.changes` in a
successful local transaction. Use `client.sync.snapshot()` for bootstrap. The
legacy object-backup `sync` category has been removed; object backups support
only `db`, `full`, `game`, and `asset`.

## Device Secret Rotation

```ts
await client.devices.rotateSecret()
```

Without an argument, `rotateSecret` uses the locally stored device ID. On
success, it stores the new `deviceSecret` in `deviceStore`.

## Same-Origin Defaults

Given:

```text
baseUrl = https://umbra.example.com
```

The SDK derives:

```text
apiBaseUrl            = https://umbra.example.com/api/v1
authorizationEndpoint = https://umbra.example.com/oauth2/auth
tokenEndpoint         = https://umbra.example.com/oauth2/token
revocationEndpoint    = https://umbra.example.com/oauth2/revoke
```

Advanced deployments can override any endpoint in the constructor config.

## Notes

- The core entry is Web API based and expects `fetch` plus Web Crypto.
- `@umbrae-labs/umbra-sdk/electron` should be used from Electron main process, not renderer.
- `FileTokenStore` and `FileDeviceCredentialStore` are for development and examples. Production apps should use OS keychain-backed stores.
- `registrationToken` and `deviceSecret` are sensitive. Do not store them in ordinary user-editable config.
- `uploadFile` and `downloadFile` stream file content in Node/Electron instead of loading whole backup files into memory.
- Loopback redirects should use `127.0.0.1`, not `localhost`, when using dynamic ports.
