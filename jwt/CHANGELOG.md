# Changelog

All notable changes to the `jwt` extension will be documented in this file.

## [1.0.0] - 2026-07-18

### Added
- Initial release of the `jwt` extension for GoStack.
- `Claims` and `Manager` structs to manage stateless authentication token lifecycles.
- `JWTGuard` implementation conforming to the core `contract.Guard` interface.
- GoStack-native `Middleware` and `GetClaims` context helper functions for route-level token validation.
- `ServiceProvider` to bind the JWT manager into the Citadel dependency container automatically.
