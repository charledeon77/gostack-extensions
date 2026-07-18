<p align="center">
  <img src="assets/images/GoStack-Extensions-Logo.jpg" alt="GoStack Extensions Logo" width="100%">
</p>

<p align="center">
  <b>GoStack Extensions</b> — <i>Official modular plugins, adapters, and integrations for the GoStack Web Framework.</i>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/License-Apache%202.0-orange?logo=apache" />
  <img src="https://img.shields.io/badge/Go-1.26.3%2B-00ADD8?logo=go" />
  <img src="https://img.shields.io/badge/Ecosystem-GoStack-1FB249" />
</p>

---

## 🔌 What are GoStack Extensions?

In web ecosystems like **Laravel (Packages)**, **Django (Apps)**, and **SvelteKit (Integrations)**, you don't always want every single feature loaded into the core framework. Keeping the core lean makes applications boot faster, run lighter, and stay maintainable.

**GoStack Extensions** are official, plug-and-play modules designed to extend the GoStack framework. They connect seamlessly with the GoStack IoC Container, allowing you to add advanced features—like Multi-Factor Authentication (MFA) or Role-Based Access Control (RBAC)—only when your application niche demands it.

---

## 🚀 How They Work

Every extension is built as a native Go Module utilizing GoStack's **ServiceProvider** pattern. When you import an extension, it registers its own controllers, database migrations, middleware, or services into your application container without you writing any glue code.

---

## 📦 Directory of Official Extensions

| Extension | Path | Purpose | Status |
| :--- | :--- | :--- | :--- |
| **MFA / 2FA** | `/mfa` | Time-based One-Time Passwords (TOTP) QR code generation & verification. | ✅ Stable |
| **RBAC** | `/rbac` | Database-backed Roles & Permissions table structure, route guards, and NoSQL/Cassandra resolvers. | ✅ Stable |
| **JWT Auth** | `/jwt` | Stateless JWT token validation, bearer token extraction, and route guard middleware. | ✅ Stable |

---

## 📥 Installation

Each extension is its own sub-module, allowing you to download only what you use. Run the `go get` command pointing to the specific sub-folder:

### MFA / 2FA
```bash
go get github.com/charledeon77/gostack-extensions/mfa
```

### RBAC (Role-Based Access Control)
```bash
go get github.com/charledeon77/gostack-extensions/rbac
```

### JWT Authentication
```bash
go get github.com/charledeon77/gostack-extensions/jwt
```

---

## 🛠️ Usage Example

Integrating an extension into your GoStack application lifecycle is as simple as registering its ServiceProvider in your bootstrap configuration (usually in your `cmd/app/main.go` or `framework/foundation` loader):

```go
package main

import (
    "github.com/charledeon77/gostack-framework/framework/foundation"
    "github.com/charledeon77/gostack-extensions/rbac" // 1. Import the extension
)

func main() {
    app := foundation.NewApplication()

    // 2. Register the extension's provider
    app.RegisterProvider(rbac.NewServiceProvider())

    app.Boot()
}
```

Once registered, the extension's middlewares, routes, and services are fully active and injected!

---

## 🛡️ Ecosystem Compatibility

These extensions are designed **exclusively** for the **GoStack Web Framework** ecosystem. They rely on GoStack's native session stores, routers, database schema builders, and auth guards.

---

## 🤝 Contributing

We welcome contributions from the community! If you have built an extension or have an idea for a first-party adapter:
1. Fork this repository.
2. Create your feature branch (`git checkout -b extension/my-awesome-feature`).
3. Commit your changes.
4. Push to the branch and open a Pull Request.

---

## 📄 License

This repository is open-sourced software licensed under the Apache 2.0 License.
