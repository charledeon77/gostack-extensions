package mfa

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestMFA_GenerateAndVerify(t *testing.T) {
	manager := NewManager("TestIssuer")

	// 1. Generate Secret
	secret, err := manager.GenerateSecret("user@gostack.dev")
	if err != nil {
		t.Fatalf("failed to generate secret: %v", err)
	}

	if secret.Secret == "" {
		t.Error("expected secret to be non-empty")
	}

	// 2. Generate Base64 QR Image
	qr, err := secret.QRImageBase64(200, 200)
	if err != nil {
		t.Fatalf("failed to generate QR code data URL: %v", err)
	}

	if qr == "" {
		t.Error("expected base64 QR URL to be non-empty")
	}

	// 3. Verify Valid Code
	code, err := totp.GenerateCode(secret.Secret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate standard verification code: %v", err)
	}

	if !manager.Verify(code, secret.Secret) {
		t.Error("expected valid passcode verification to pass")
	}

	// 4. Verify Invalid Code
	if manager.Verify("123456", secret.Secret) {
		t.Error("expected invalid passcode verification to fail")
	}
}
