package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charledeon77/gostack-framework/framework/contract"
	"github.com/charledeon77/gostack-framework/framework/database"
	httpFrame "github.com/charledeon77/gostack-framework/framework/http"
	_ "modernc.org/sqlite"
)

// mockUser satisfies contract.Authenticatable
type mockUser struct {
	id any
}

func (u *mockUser) GetID() any         { return u.id }
func (u *mockUser) GetEmail() string    { return "" }
func (u *mockUser) GetPassword() string { return "" }

func TestRBAC_InMemory(t *testing.T) {
	manager := NewManager(nil)

	// Define role and permissions
	manager.DefineRole("admin", []string{"create-post", "delete-post"})
	manager.DefineRole("editor", []string{"create-post"})

	// Assign roles to users
	manager.AssignRole(1, "admin")
	manager.AssignRole(2, "editor")

	// Verify HasRole
	if !manager.HasRole(1, "admin") {
		t.Error("expected user 1 to have admin role")
	}
	if manager.HasRole(1, "editor") {
		t.Error("expected user 1 to not have editor role")
	}
	if !manager.HasRole(2, "editor") {
		t.Error("expected user 2 to have editor role")
	}

	// Verify HasPermission / Check
	if !manager.HasPermission(1, "delete-post") {
		t.Error("expected user 1 to have permission delete-post")
	}
	if manager.HasPermission(2, "delete-post") {
		t.Error("expected user 2 to not have permission delete-post")
	}
	if !manager.Check(2, "create-post") {
		t.Error("expected user 2 to have permission create-post (check)")
	}

	// Remove role
	manager.RemoveRole(1, "admin")
	if manager.HasRole(1, "admin") {
		t.Error("expected user 1 to not have admin role after removal")
	}
	if manager.HasPermission(1, "delete-post") {
		t.Error("expected user 1 to not have permission delete-post after role removal")
	}
}

func TestRBAC_CustomResolvers(t *testing.T) {
	manager := NewManager(nil)

	// Set dynamic resolvers
	manager.SetRoleResolver(func(userID any) ([]string, error) {
		if userID == 42 {
			return []string{"manager"}, nil
		}
		return nil, nil
	})

	manager.SetPermissionResolver(func(userID any) ([]string, error) {
		if userID == 42 {
			return []string{"approve-leave"}, nil
		}
		return nil, nil
	})

	// Verify lookups
	if !manager.HasRole(42, "manager") {
		t.Error("expected resolver to return manager role for user 42")
	}
	if manager.HasRole(99, "manager") {
		t.Error("expected resolver to not return manager role for user 99")
	}
	if !manager.HasPermission(42, "approve-leave") {
		t.Error("expected resolver to return approve-leave permission for user 42")
	}
}

func TestRBAC_DatabaseSQL(t *testing.T) {
	// Initialize an in-memory SQLite database
	db, err := database.NewSQLAdapter("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to initialize sqlite connection pool: %v", err)
	}
	if err := db.Connect(); err != nil {
		t.Fatalf("failed to connect to sqlite: %v", err)
	}
	defer db.Close()

	// Provision tables
	err = db.Exec("CREATE TABLE roles (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("failed to create roles table: %v", err)
	}
	err = db.Exec("CREATE TABLE role_user (user_id INTEGER, role_id INTEGER)")
	if err != nil {
		t.Fatalf("failed to create role_user pivot table: %v", err)
	}
	err = db.Exec("CREATE TABLE permissions (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("failed to create permissions table: %v", err)
	}
	err = db.Exec("CREATE TABLE permission_role (permission_id INTEGER, role_id INTEGER)")
	if err != nil {
		t.Fatalf("failed to create permission_role pivot table: %v", err)
	}

	// Seed roles and relations
	_ = db.Exec("INSERT INTO roles (id, name) VALUES (1, 'administrator')")
	_ = db.Exec("INSERT INTO role_user (user_id, role_id) VALUES (100, 1)")
	_ = db.Exec("INSERT INTO permissions (id, name) VALUES (10, 'write-code')")
	_ = db.Exec("INSERT INTO permission_role (permission_id, role_id) VALUES (10, 1)")

	// Create manager and activate DB check
	manager := NewManager(db)
	manager.SetUseDB(true)

	// Verify database backed lookups
	if !manager.HasRole(100, "administrator") {
		t.Error("expected database query to find administrator role for user 100")
	}
	if manager.HasRole(200, "administrator") {
		t.Error("expected database query to not find administrator role for user 200")
	}

	// Define role permissions in-memory mapping to DB-resolved role
	manager.DefineRole("administrator", []string{"write-code"})

	if !manager.HasPermission(100, "write-code") {
		t.Error("expected user 100 with database role administrator to carry in-memory role permission write-code")
	}

	// Verify DB direct permissions
	if !manager.HasPermission(100, "write-code") {
		t.Error("expected DB query fallback to return true for user 100 direct permissions")
	}
}

func TestRBAC_Middlewares(t *testing.T) {
	manager := NewManager(nil)
	manager.DefineRole("admin", []string{"delete-user"})
	manager.AssignRole(42, "admin")

	bootstrapCtx := func(user contract.Authenticatable, acceptJSON bool) (*httpFrame.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		if acceptJSON {
			req.Header.Set("Accept", "application/json")
		}

		// Inject authenticated user cache container
		cache := &contract.AuthCache{
			User:   user,
			Loaded: true,
		}
		reqCtx := context.WithValue(req.Context(), contract.UserContextKey, cache)
		req = req.WithContext(reqCtx)

		ctx := &httpFrame.Context{
			Writer:  w,
			Request: req,
		}
		return ctx, w
	}

	// 1. RequireRole Middleware — Authorized
	ctx, w := bootstrapCtx(&mockUser{id: 42}, false)
	nextCalled := false
	mw := RequireRole(manager, "admin")
	err := mw(ctx, func(c *httpFrame.Context) error {
		nextCalled = true
		return nil
	})
	if err != nil || !nextCalled {
		t.Errorf("expected authorized user to pass RequireRole middleware, error: %v", err)
	}

	// 2. RequireRole Middleware — Forbidden Web
	ctx, w = bootstrapCtx(&mockUser{id: 99}, false) // ID 99 does not have admin role
	nextCalled = false
	_ = mw(ctx, func(c *httpFrame.Context) error {
		nextCalled = true
		return nil
	})
	if nextCalled || w.Code != http.StatusForbidden {
		t.Errorf("expected unauthorized role user to be blocked (403), got code: %d", w.Code)
	}

	// 3. RequireRole Middleware — Forbidden API
	ctx, w = bootstrapCtx(&mockUser{id: 99}, true) // JSON request
	nextCalled = false
	_ = mw(ctx, func(c *httpFrame.Context) error {
		nextCalled = true
		return nil
	})
	if nextCalled || w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"error":"Forbidden"`) {
		t.Errorf("expected API forbidden response (403), got code: %d, body: %s", w.Code, w.Body.String())
	}

	// 4. RequirePermission Middleware — Authorized
	ctx, w = bootstrapCtx(&mockUser{id: 42}, false)
	nextCalled = false
	mwPerm := RequirePermission(manager, "delete-user")
	err = mwPerm(ctx, func(c *httpFrame.Context) error {
		nextCalled = true
		return nil
	})
	if err != nil || !nextCalled {
		t.Errorf("expected authorized permission user to pass RequirePermission middleware, error: %v", err)
	}

	// 5. RequirePermission Middleware — Unauthenticated Web (should redirect to /login)
	ctx, w = bootstrapCtx(nil, false) // unauthenticated user
	nextCalled = false
	_ = mwPerm(ctx, func(c *httpFrame.Context) error {
		nextCalled = true
		return nil
	})
	if nextCalled || w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Errorf("expected unauthenticated web request to redirect to /login (302), got code: %d, location: %s", w.Code, w.Header().Get("Location"))
	}

	// 6. RequirePermission Middleware — Unauthenticated API (should return 401 JSON)
	ctx, w = bootstrapCtx(nil, true) // unauthenticated API request
	nextCalled = false
	_ = mwPerm(ctx, func(c *httpFrame.Context) error {
		nextCalled = true
		return nil
	})
	if nextCalled || w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"error":"Unauthorized"`) {
		t.Errorf("expected unauthenticated API request to return 401 JSON, got code: %d, body: %s", w.Code, w.Body.String())
	}
}
