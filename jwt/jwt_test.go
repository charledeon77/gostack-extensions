package jwt

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/charledeon77/gostack-framework/framework/contract"
	httpFrame "github.com/charledeon77/gostack-framework/framework/http"
)

// ─── MOCKS ───────────────────────────────────────────────────────────────────

type mockUser struct {
	id    string
	email string
}

func (u *mockUser) GetID() any         { return u.id }
func (u *mockUser) GetEmail() string   { return u.email }
func (u *mockUser) GetPassword() string { return "" }

type mockUserProvider struct {
	users map[string]*mockUser
}

func (p *mockUserProvider) RetrieveByID(id any) (contract.Authenticatable, error) {
	idStr, ok := id.(string)
	if !ok {
		return nil, errors.New("invalid ID type")
	}
	user, exists := p.users[idStr]
	if !exists {
		return nil, errors.New("user not found")
	}
	return user, nil
}

func (p *mockUserProvider) RetrieveByCredentials(credentials map[string]any) (contract.Authenticatable, error) {
	return nil, nil
}

func (p *mockUserProvider) ValidateCredentials(user contract.Authenticatable, credentials map[string]any) bool {
	return false
}

// ─── TESTS ───────────────────────────────────────────────────────────────────

func TestJWTGenerateAndVerify(t *testing.T) {
	secret := "my-secret-key-12345"
	manager := NewManager(secret, "HS256", 10*time.Minute)

	token, err := manager.Generate("user_42", "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("failed to verify token: %v", err)
	}

	if claims.UserID != "user_42" {
		t.Errorf("expected UserID to be 'user_42', got '%s'", claims.UserID)
	}

	if claims.Role != "admin" {
		t.Errorf("expected Role to be 'admin', got '%s'", claims.Role)
	}
}

func TestJWTExpiredToken(t *testing.T) {
	secret := "my-secret-key-12345"
	// Use a large negative TTL (1 hour in the past) to guarantee the token
	// is already expired when we verify it, avoiding second-precision clock edge cases.
	manager := NewManager(secret, "HS256", -1*time.Hour)

	token, err := manager.Generate("user_42", "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	_, err = manager.Verify(token)
	if err == nil {
		t.Fatal("expected error verifying expired token, got nil")
	}
}

func TestJWTTamperedSignature(t *testing.T) {
	secret := "my-secret-key-12345"
	manager := NewManager(secret, "HS256", 10*time.Minute)

	token, err := manager.Generate("user_42", "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Tamper with the token
	tamperedToken := token + "a"

	_, err = manager.Verify(tamperedToken)
	if err == nil {
		t.Fatal("expected error verifying tampered token, got nil")
	}
}

func TestJWTGuard(t *testing.T) {
	secret := "my-secret-key-12345"
	manager := NewManager(secret, "HS256", 10*time.Minute)
	provider := &mockUserProvider{
		users: map[string]*mockUser{
			"user_42": {id: "user_42", email: "user42@example.com"},
		},
	}
	guard := NewJWTGuard(manager, provider)

	token, err := manager.Generate("user_42", "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	// Pre-populate context UserContextKey cache structure just like GoStack kernel
	cache := &contract.AuthCache{}
	ctxReq := req.WithContext(context.WithValue(req.Context(), contract.UserContextKey, cache))

	user, ok := guard.User(ctxReq)
	if !ok {
		t.Fatal("expected guard.User to return true")
	}

	if user.GetID() != "user_42" {
		t.Errorf("expected user ID 'user_42', got '%v'", user.GetID())
	}

	if !guard.Check(ctxReq) {
		t.Error("expected guard.Check to be true")
	}
}

func TestJWTMiddleware(t *testing.T) {
	secret := "my-secret-key-12345"
	manager := NewManager(secret, "HS256", 10*time.Minute)
	provider := &mockUserProvider{
		users: map[string]*mockUser{
			"user_42": {id: "user_42", email: "user42@example.com"},
		},
	}
	middleware := Middleware(manager, provider)

	token, err := manager.Generate("user_42", "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	ctx := &httpFrame.Context{
		Writer:  w,
		Request: req,
	}

	called := false
	next := func(c *httpFrame.Context) error {
		called = true
		return nil
	}

	err = middleware(ctx, next)
	if err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	if !called {
		t.Fatal("next handler was not called")
	}

	claims := GetClaims(ctx)
	if claims == nil {
		t.Fatal("expected claims to be populated in context")
	}

	if claims.UserID != "user_42" {
		t.Errorf("expected user_id to be 'user_42', got '%s'", claims.UserID)
	}

	val := ctx.Get("user_id")
	if val != "user_42" {
		t.Errorf("expected context user_id 'user_42', got '%v'", val)
	}

	cacheVal := ctx.Request.Context().Value(contract.UserContextKey)
	if cacheVal == nil {
		t.Fatal("expected request context user cache to be initialized")
	}
	cache := cacheVal.(*contract.AuthCache)
	if !cache.Loaded || cache.User == nil || cache.User.GetID() != "user_42" {
		t.Error("expected context cache to contain the hydrated user")
	}
}
