package rbac

import (
	"database/sql"
	"net/http"
	"strings"
	"sync"

	"github.com/charledeon77/gostack-framework/framework/contract"
	"github.com/charledeon77/gostack-framework/framework/foundation"
	httpFrame "github.com/charledeon77/gostack-framework/framework/http"
)

// Manager coordinates roles, permissions, dynamic resolvers, and database queries.
type Manager struct {
	mu                  sync.RWMutex
	db                  contract.Database
	inMemRoles          map[string][]string // role -> permissions
	inMemUserRoles      map[any][]string    // user ID -> roles
	useDB               bool
	roleResolver        func(userID any) ([]string, error)
	permissionResolver  func(userID any) ([]string, error)
	UserRolesQuery      string
	UserPermissionQuery string
}

// NewManager creates a new instance of the RBAC Manager.
func NewManager(db contract.Database) *Manager {
	return &Manager{
		db:                  db,
		inMemRoles:          make(map[string][]string),
		inMemUserRoles:      make(map[any][]string),
		UserRolesQuery:      "SELECT r.name FROM roles r JOIN role_user ru ON r.id = ru.role_id WHERE ru.user_id = ?",
		UserPermissionQuery: "SELECT p.name FROM permissions p JOIN permission_role pr ON p.id = pr.permission_id JOIN role_user ru ON pr.role_id = ru.role_id WHERE ru.user_id = ?",
	}
}

// SetUseDB enables or disables database lookups for roles and permissions.
func (m *Manager) SetUseDB(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.useDB = enabled
}

// SetRoleResolver registers a custom callback to resolve user roles dynamically.
// Useful for NoSQL and Cassandra databases.
func (m *Manager) SetRoleResolver(fn func(userID any) ([]string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roleResolver = fn
}

// SetPermissionResolver registers a custom callback to resolve user permissions dynamically.
// Useful for NoSQL and Cassandra databases.
func (m *Manager) SetPermissionResolver(fn func(userID any) ([]string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permissionResolver = fn
}

// DefineRole associates a role with a slice of permissions.
func (m *Manager) DefineRole(role string, permissions []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inMemRoles[role] = permissions
}

// AssignRole assigns a role to a specific user ID in-memory.
func (m *Manager) AssignRole(userID any, role string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inMemUserRoles[userID] = append(m.inMemUserRoles[userID], role)
}

// RemoveRole removes an assigned role from a specific user ID in-memory.
func (m *Manager) RemoveRole(userID any, role string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	roles, ok := m.inMemUserRoles[userID]
	if !ok {
		return
	}
	var updated []string
	for _, r := range roles {
		if r != role {
			updated = append(updated, r)
		}
	}
	m.inMemUserRoles[userID] = updated
}

// HasRole checks if the given user ID carries the specified role.
func (m *Manager) HasRole(userID any, role string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. Check custom role resolver first (e.g. for Cassandra/NoSQL engines)
	if m.roleResolver != nil {
		roles, err := m.roleResolver(userID)
		if err == nil {
			for _, r := range roles {
				if r == role {
					return true
				}
			}
		}
	}

	// 2. Check in-memory maps
	if roles, ok := m.inMemUserRoles[userID]; ok {
		for _, r := range roles {
			if r == role {
				return true
			}
		}
	}

	// 3. Fall back to SQL database query if enabled
	if m.useDB && m.db != nil {
		rowsObj, err := m.db.Query(m.UserRolesQuery, userID)
		if err == nil {
			if rows, ok := rowsObj.(*sql.Rows); ok {
				defer rows.Close()
				for rows.Next() {
					var r string
					if err := rows.Scan(&r); err == nil && r == role {
						return true
					}
				}
			}
		}
	}

	return false
}

// HasPermission checks if the given user ID is authorized with the specified permission.
func (m *Manager) HasPermission(userID any, permission string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. Check custom permission resolver first (e.g. for Cassandra/NoSQL engines)
	if m.permissionResolver != nil {
		permissions, err := m.permissionResolver(userID)
		if err == nil {
			for _, p := range permissions {
				if p == permission {
					return true
				}
			}
		}
	}

	// 2. Resolve roles to check permissions in-memory
	var roles []string

	// Load roles from custom resolver
	if m.roleResolver != nil {
		if r, err := m.roleResolver(userID); err == nil {
			roles = append(roles, r...)
		}
	}

	// Load roles from in-memory map
	if r, ok := m.inMemUserRoles[userID]; ok {
		roles = append(roles, r...)
	}

	// Load roles from SQL database query
	if m.useDB && m.db != nil {
		rowsObj, err := m.db.Query(m.UserRolesQuery, userID)
		if err == nil {
			if rows, ok := rowsObj.(*sql.Rows); ok {
				defer rows.Close()
				for rows.Next() {
					var r string
					if err := rows.Scan(&r); err == nil {
						roles = append(roles, r)
					}
				}
			}
		}
	}

	// Match permissions bound to roles in-memory
	for _, r := range roles {
		if permissions, ok := m.inMemRoles[r]; ok {
			for _, p := range permissions {
				if p == permission {
					return true
				}
			}
		}
	}

	// 3. Fall back to custom user direct permissions lookup in SQL database
	if m.useDB && m.db != nil {
		rowsObj, err := m.db.Query(m.UserPermissionQuery, userID)
		if err == nil {
			if rows, ok := rowsObj.(*sql.Rows); ok {
				defer rows.Close()
				for rows.Next() {
					var p string
					if err := rows.Scan(&p); err == nil && p == permission {
						return true
					}
				}
			}
		}
	}

	return false
}

// Check is a generic alias checking if a user has a specific permission.
func (m *Manager) Check(userID any, permission string) bool {
	return m.HasPermission(userID, permission)
}

// ─── MIDDLEWARES ─────────────────────────────────────────────────────────────

// RequireRole blocks access if the authenticated user does not carry the specified role.
func RequireRole(manager *Manager, role string) httpFrame.Middleware {
	return func(ctx *httpFrame.Context, next httpFrame.NextHandler) error {
		user, ok := getUserFromContext(ctx.Request)
		if !ok {
			return unauthorized(ctx)
		}

		if !manager.HasRole(user.GetID(), role) {
			return forbidden(ctx)
		}

		return next(ctx)
	}
}

// RequirePermission blocks access if the authenticated user does not carry the specified permission.
func RequirePermission(manager *Manager, permission string) httpFrame.Middleware {
	return func(ctx *httpFrame.Context, next httpFrame.NextHandler) error {
		user, ok := getUserFromContext(ctx.Request)
		if !ok {
			return unauthorized(ctx)
		}

		if !manager.HasPermission(user.GetID(), permission) {
			return forbidden(ctx)
		}

		return next(ctx)
	}
}

func getUserFromContext(r *http.Request) (contract.Authenticatable, bool) {
	if cacheVal := r.Context().Value(contract.UserContextKey); cacheVal != nil {
		if cache, ok := cacheVal.(*contract.AuthCache); ok && cache.Loaded {
			return cache.User, cache.User != nil
		}
	}
	return nil, false
}

func unauthorized(ctx *httpFrame.Context) error {
	accept := ctx.Request.Header.Get("Accept")
	contentType := ctx.Request.Header.Get("Content-Type")
	isAPI := strings.Contains(accept, "application/json") || 
		strings.Contains(contentType, "application/json")

	if isAPI {
		return ctx.JSON(http.StatusUnauthorized, map[string]string{
			"error": "Unauthorized",
		})
	}

	ctx.Writer.Header().Set("Location", "/login")
	ctx.Writer.WriteHeader(http.StatusFound)
	return nil
}

func forbidden(ctx *httpFrame.Context) error {
	accept := ctx.Request.Header.Get("Accept")
	contentType := ctx.Request.Header.Get("Content-Type")
	isAPI := strings.Contains(accept, "application/json") || 
		strings.Contains(contentType, "application/json")

	if isAPI {
		return ctx.JSON(http.StatusForbidden, map[string]string{
			"error": "Forbidden",
		})
	}

	ctx.Writer.WriteHeader(http.StatusForbidden)
	_, _ = ctx.Writer.Write([]byte("Forbidden"))
	return nil
}

// ─── SERVICE PROVIDER ─────────────────────────────────────────────────────────

// ServiceProvider integrates the RBAC system into the GoStack IoC registry.
type ServiceProvider struct{}

// NewServiceProvider builds a new RBAC provider.
func NewServiceProvider() *ServiceProvider {
	return &ServiceProvider{}
}

// Register registers the RBAC manager blueprint to the container.
func (sp *ServiceProvider) Register(c *foundation.Container) {
	c.BindSingleton("rbac", func(container *foundation.Container) any {
		var db contract.Database
		if dbVal, err := container.Resolve("db"); err == nil && dbVal != nil {
			db = dbVal.(contract.Database)
		}

		useDB := false
		if cfgVal, err := container.Resolve("config"); err == nil && cfgVal != nil {
			if cfg, ok := cfgVal.(interface{ GetOrDefault(string, string) string }); ok {
				if cfg.GetOrDefault("RBAC_DB_ENABLED", "false") == "true" {
					useDB = true
				}
			}
		}

		manager := NewManager(db)
		manager.SetUseDB(useDB)
		return manager
	})
}

// Boot is a no-op method conforming to ServiceProvider lifecycle protocols.
func (sp *ServiceProvider) Boot(c *foundation.Container) {}
