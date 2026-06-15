# Umbra SDK

Umbra SDK is a set of client libraries for integrating game managers with
Umbra backup services.

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
- Signed requests for protected backup APIs
- Backup upload and download helpers
- Same-origin endpoint defaults for Umbra deployments
