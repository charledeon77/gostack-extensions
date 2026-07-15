# Changelog

All notable changes to the GoStack MFA extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-07-15
### Added
- **Initial Release**: Launched the official GoStack MFA (2FA) extension.
- **TOTP Generation**: Support for generating cryptographically secure base32 TOTP secrets.
- **QR Code Generation**: Direct generation of Base64 encoded PNG QR code URLs for quick frontend rendering.
- **Verification Engine**: Built-in validation of 6-digit TOTP passcodes using RFC 6238 standards.
- **Service Provider**: Automatic registry of the `mfa` manager into the GoStack IoC container.
