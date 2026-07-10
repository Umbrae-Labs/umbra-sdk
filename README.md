# Umbra SDK

Umbra SDK is a set of client libraries for integrating game managers with
Umbra backup and structured synchronization services.

It provides a unified way to connect desktop game managers to Umbra account
authorization, device registration, signed backup APIs, and upload/download
workflows. Support popular desktop development frameworks such as Electron, Tauri, and Walis

## Packages

| SDK | Directory | Package |
| --- | --- | --- |
| TypeScript | `umbra-typescript` | `@umbrae-labs/umbra-sdk` |
| Rust | `umbra-rust` | `umbra-sdk` |
| Go | `umbra-go` | `github.com/Umbrae-Labs/umbra-sdk/umbra-go` |

## What It Handles

- OAuth login for desktop clients
- Device registration and device secret rotation
- Signed requests for protected backup and sync APIs
- Backup upload and download helpers
- Generic JSON exchange and snapshot clients with explicit conflict results
- Same-origin endpoint defaults for Umbra deployments

Structured synchronization is exposed independently from object backup in all
three SDKs. It does not control DuckDB, SQLite, or application merge policy.
Object backup categories are `db`, `full`, `game`, and `asset`; the legacy
`sync` backup triple has been removed.
