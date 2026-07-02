/*
Purpose:
This file implements the GoStack MFA/2FA (TOTP) extension. It handles generating
cryptographic secrets, constructing QR codes for authenticator apps, and verifying
time-based passcodes.

Philosophy:
Security should be easy to implement and bulletproof. By using standard TOTP algorithms,
we ensure full compatibility with Google Authenticator, Authy, and Microsoft Authenticator
while offering developers a single, elegant interface to enforce MFA.

Architecture:
Designed as a plug-and-play ServiceProvider for the GoStack framework. It registers
the TOTP Manager in the IoC container so any application controller can resolve and use it.

Choice:
We use `github.com/pquerna/otp/totp` because it is the most stable, secure, and widely
used TOTP library in the Go ecosystem, adhering perfectly to standard RFC 6238.

Implementation:
- Secret: struct representing the generated 2FA registration keys and QR URL.
- Manager: service wrapping code generation and verification.
- ServiceProvider: registers the manager into the GoStack IoC container.
*/
package mfa

import (
	"bytes"
	"encoding/base64"
	"image/png"

	"github.com/charledeon77/gostack-framework/framework/foundation"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// Secret holds the generated MFA details for registering a new device.
type Secret struct {
	Secret string // The raw base32 encoded secret key
	URL    string // The provisioning URL (otpauth://...)
}

// QRImageBase64 returns the QR code image encoded as a Base64 data URL string.
func (s *Secret) QRImageBase64(width, height int) (string, error) {
	key, err := otp.NewKeyFromURL(s.URL)
	if err != nil {
		return "", err
	}

	img, err := key.Image(width, height)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	return "data:image/png;base64," + encoded, nil
}

// Manager handles the creation and verification of TOTP 2FA.
type Manager struct {
	issuer string
}

// NewManager creates a new TOTP verification manager.
func NewManager(issuer string) *Manager {
	if issuer == "" {
		issuer = "GoStack"
	}
	return &Manager{issuer: issuer}
}

// GenerateSecret creates a new secret key and QR URL for a user.
func (m *Manager) GenerateSecret(accountName string) (*Secret, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      m.issuer,
		AccountName: accountName,
	})
	if err != nil {
		return nil, err
	}

	return &Secret{
		Secret: key.Secret(),
		URL:    key.String(),
	}, nil
}

// Verify checks if the given passcode is valid for the secret at the current time.
func (m *Manager) Verify(passcode, secret string) bool {
	return totp.Validate(passcode, secret)
}

// ServiceProvider integrates the MFA system into the GoStack application.
type ServiceProvider struct{}

func NewServiceProvider() *ServiceProvider {
	return &ServiceProvider{}
}

// Register binds the MFA Manager singleton into the Service Container.
func (sp *ServiceProvider) Register(c *foundation.Container) {
	c.BindSingleton("mfa", func(container *foundation.Container) any {
		// Reads issuer from app config, falling back to "GoStack"
		issuer := "GoStack"
		if cfgVal, err := container.Resolve("config"); err == nil && cfgVal != nil {
			if cfg, ok := cfgVal.(interface{ GetOrDefault(string, string) string }); ok {
				issuer = cfg.GetOrDefault("APP_NAME", "GoStack")
			}
		}
		return NewManager(issuer)
	})
}

// Boot is a no-op lifecycle implementation for contract compliance.
func (sp *ServiceProvider) Boot(c *foundation.Container) {}
