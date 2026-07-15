# Changelog

All notable changes to the GoStack RBAC extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.1] - 2026-07-15
### Added
- **SuperAdmin Bypass Roles**: Introduced `AddBypassRole` and `RemoveBypassRole` to enable root roles (like `superadmin`) to bypass all permission and role validations automatically.
- **User Direct Permissions**: Added `AssignPermissionToUser` and `RemovePermissionFromUser` allowing permissions to be assigned directly to specific user IDs in-memory, bypassing roles.
- **Contextual Rules (Meta-Capabilities)**: Added `DefineRule` and updated `HasPermission` / `Check` to accept variadic `context ...any` arguments, allowing developers to implement dynamic validation checks (such as resource/post ownership).

### Fixed
- **PostgreSQL Placeholder Support**: Configured `NewManager` to automatically query the `db.Driver()` at startup and update default queries to use `$1` instead of `?` if the database is running on PostgreSQL or CockroachDB.

## [1.0.0] - 2026-07-15
### Added
- **Initial Release**: Launched the official GoStack RBAC extension module.
- **In-Memory RBAC**: In-memory role-to-permission mapping (`DefineRole`) and user role assignments (`AssignRole`/`RemoveRole`).
- **Dynamic Resolvers**: Custom role and permission resolvers (`SetRoleResolver`/`SetPermissionResolver`) supporting NoSQL and Cassandra databases.
- **Database Fallbacks**: Relational database query lookups for checking roles and permissions.
- **GoStack Middlewares**: Route guards (`RequireRole`/`RequirePermission`) leveraging GoStack's authenticated user cache.
- **Service Provider Integration**: Container registry support for singleton IoC registration.
