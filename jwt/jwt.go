/*
Purpose:
This file implements the GoStack JWT authentication extension. It provides claims structures,
a manager to sign and verify tokens statelessly, a compliant contract.Guard wrapper,
and standard HTTP middleware for request-scoped context authentication.

Philosophy:
Stateless authentication should integrate seamlessly with GoStack's container (Citadel)
and request lifecycle context cache. By writing a JWT manager and guard that satisfies the
framework's contracts, developers get zero-overhead authentication without breaking 
downstream roles, permissions, or session checks.

Choice:
We use `github.com/golang-jwt/jwt/v5` as a standard, secure base dependency to sign tokens,
while presenting a highly simplified, GoStack-native interface containingHS256 defaults.
*/
package jwt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charledeon77/gostack-framework/framework/contract"
	"github.com/charledeon77/gostack-framework/framework/foundation"
	httpFrame "github.com/charledeon77/gostack-framework/framework/http"
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT payload metadata.
type Claims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// Manager coordinates token generation and verification using configurable keys and algorithms.
type Manager struct {
	secret []byte
	algo   string
	ttl    time.Duration
}

// NewManager initializes a JWT manager with configuration settings.
func NewManager(secret string, algo string, ttl time.Duration) *Manager {
	if algo == "" {
		algo = "HS256"
	}
	// Only apply the default TTL when the caller provides exactly zero.
	// A negative value is intentionally allowed (e.g. for testing already-expired tokens).
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	return &Manager{
		secret: []byte(secret),
		algo:   algo,
		ttl:    ttl,
	}
}

// Generate signs and returns a new HS256 JWT containing the user's ID and role claims.
func (m *Manager) Generate(userID string, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	var token *jwt.Token
	switch strings.ToUpper(m.algo) {
	case "HS384":
		token = jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	case "HS512":
		token = jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	default: // Default to HS256
		token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	}

	return token.SignedString(m.secret)
}

// Verify parses, validates, and returns the claims from a JWT string.
func (m *Manager) Verify(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		switch token.Method.(type) {
		case *jwt.SigningMethodHMAC:
			return m.secret, nil
		default:
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// GetClaims safely extracts validated JWT claims from the GoStack HTTP request Context.
func GetClaims(ctx *httpFrame.Context) *Claims {
	if val := ctx.Get("jwt_claims"); val != nil {
		if claims, ok := val.(*Claims); ok {
			return claims
		}
	}
	return nil
}

// JWTGuard implements contract.Guard statelessly using the JWT manager.
type JWTGuard struct {
	manager  *Manager
	provider contract.UserProvider
}

// NewJWTGuard creates a new JWTGuard adapter instance.
func NewJWTGuard(manager *Manager, provider contract.UserProvider) *JWTGuard {
	return &JWTGuard{
		manager:  manager,
		provider: provider,
	}
}

// Check validates if the request context contains a valid JWT user signature.
func (g *JWTGuard) Check(r *http.Request) bool {
	_, ok := g.User(r)
	return ok
}

// User extracts the JWT token, verifies it, hydrates the Authenticatable user entity, and caches it.
func (g *JWTGuard) User(r *http.Request) (contract.Authenticatable, bool) {
	// 1. Inspect request context cache first
	if cacheVal := r.Context().Value(contract.UserContextKey); cacheVal != nil {
		if cache, ok := cacheVal.(*contract.AuthCache); ok && cache.Loaded {
			return cache.User, cache.User != nil
		}
	}

	// 2. Extract Bearer token
	authHeader := r.Header.Get("Authorization")
	var tokenStr string
	if authHeader != "" && strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		tokenStr = strings.TrimSpace(authHeader[7:])
	} else {
		tokenStr = r.URL.Query().Get("api_token")
	}

	if tokenStr == "" {
		g.cacheUser(r, nil)
		return nil, false
	}

	// 3. Verify JWT
	claims, err := g.manager.Verify(tokenStr)
	if err != nil {
		g.cacheUser(r, nil)
		return nil, false
	}

	// 4. Retrieve user entity from Provider
	user, err := g.provider.RetrieveByID(claims.UserID)
	if err != nil || user == nil {
		g.cacheUser(r, nil)
		return nil, false
	}

	g.cacheUser(r, user)
	return user, true
}

// Login is a stateless no-op for JWTGuard.
func (g *JWTGuard) Login(w http.ResponseWriter, r *http.Request, user contract.Authenticatable) error {
	g.cacheUser(r, user)
	return nil
}

// Logout is a stateless no-op for JWTGuard.
func (g *JWTGuard) Logout(w http.ResponseWriter, r *http.Request) error {
	g.cacheUser(r, nil)
	return nil
}

func (g *JWTGuard) cacheUser(r *http.Request, user contract.Authenticatable) {
	if cacheVal := r.Context().Value(contract.UserContextKey); cacheVal != nil {
		if cache, ok := cacheVal.(*contract.AuthCache); ok {
			cache.User = user
			cache.Loaded = true
		}
	}
}

// Middleware returns a GoStack-compliant http.Middleware interceptor.
func Middleware(manager *Manager, provider contract.UserProvider) httpFrame.Middleware {
	return func(ctx *httpFrame.Context, next httpFrame.NextHandler) error {
		authHeader := ctx.Request.Header.Get("Authorization")
		var tokenStr string

		if authHeader != "" && strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			tokenStr = strings.TrimSpace(authHeader[7:])
		} else {
			tokenStr = ctx.Request.URL.Query().Get("api_token")
		}

		if tokenStr == "" {
			return next(ctx)
		}

		claims, err := manager.Verify(tokenStr)
		if err != nil {
			return next(ctx)
		}

		user, err := provider.RetrieveByID(claims.UserID)
		if err != nil || user == nil {
			return next(ctx)
		}

		req := ctx.Request
		cacheVal := req.Context().Value(contract.UserContextKey)
		var cache *contract.AuthCache
		if cacheVal == nil {
			cache = &contract.AuthCache{}
			ctx.Request = req.WithContext(context.WithValue(req.Context(), contract.UserContextKey, cache))
		} else {
			cache = cacheVal.(*contract.AuthCache)
		}

		cache.User = user
		cache.Loaded = true

		ctx.Set("user_id", claims.UserID)
		ctx.Set("jwt_claims", claims)

		return next(ctx)
	}
}

// ServiceProvider integrates JWT into the GoStack dependency container framework.
type ServiceProvider struct{}

func NewServiceProvider() *ServiceProvider {
	return &ServiceProvider{}
}

// Register binds the JWT Manager singleton into the container.
func (sp *ServiceProvider) Register(c *foundation.Container) {
	c.BindSingleton("jwt", func(container *foundation.Container) any {
		secret := "gostack-jwt-secret-placeholder"
		algo := "HS256"
		ttl := 15 * time.Minute

		if cfgVal, err := container.Resolve("config"); err == nil && cfgVal != nil {
			if cfg, ok := cfgVal.(interface {
				GetOrDefault(string, string) string
			}); ok {
				secret = cfg.GetOrDefault("JWT_SECRET", secret)
				algo = cfg.GetOrDefault("JWT_ALGORITHM", algo)
				if ttlStr := cfg.GetOrDefault("JWT_TTL", ""); ttlStr != "" {
					if d, err := time.ParseDuration(ttlStr); err == nil {
						ttl = d
					}
				}
			}
		}

		return NewManager(secret, algo, ttl)
	})
}

// Boot is a no-op lifecycle implementation for contract compliance.
func (sp *ServiceProvider) Boot(c *foundation.Container) {}
