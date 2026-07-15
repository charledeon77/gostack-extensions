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

type mockPost struct {
	ID       int
	AuthorID int
}

func TestRBAC_PostgreSQLPlaceholder(t *testing.T) {
	// Create mock postgres db adapter implementing contract.Database
	mockDB := &mockPostgresDB{}
	manager := NewManager(mockDB)

	expectedRolesQuery := "SELECT r.name FROM roles r JOIN role_user ru ON r.id = ru.role_id WHERE ru.user_id = $1"
	if manager.UserRolesQuery != expectedRolesQuery {
		t.Errorf("expected UserRolesQuery to be %q, got %q", expectedRolesQuery, manager.UserRolesQuery)
	}

	expectedPermissionQuery := "SELECT p.name FROM permissions p JOIN permission_role pr ON p.id = pr.permission_id JOIN role_user ru ON pr.role_id = ru.role_id WHERE ru.user_id = $1"
	if manager.UserPermissionQuery != expectedPermissionQuery {
		t.Errorf("expected UserPermissionQuery to be %q, got %q", expectedPermissionQuery, manager.UserPermissionQuery)
	}
}

type mockPostgresDB struct {
	contract.Database
}

func (m *mockPostgresDB) Driver() string {
	return "postgres"
}

func TestRBAC_SuperAdminBypass(t *testing.T) {
	manager := NewManager(nil)
	manager.AddBypassRole("superadmin")

	// Assign superadmin role to user 99
	manager.AssignRole(99, "superadmin")

	// Superadmin should bypass HasRole check and have "editor" role implicitly
	if !manager.HasRole(99, "editor") {
		t.Error("expected superadmin to bypass and have editor role")
	}

	// Superadmin should bypass HasPermission check
	if !manager.HasPermission(99, "delete-everything") {
		t.Error("expected superadmin to bypass and have delete-everything permission")
	}

	// Double check that normal user 10 does not have these
	if manager.HasRole(10, "editor") {
		t.Error("expected normal user to not have editor role")
	}
	if manager.HasPermission(10, "delete-everything") {
		t.Error("expected normal user to not have delete-everything permission")
	}

	// Remove bypass role from list and check that bypass functionality is gone
	manager.RemoveBypassRole("superadmin")
	if manager.HasRole(99, "editor") {
		t.Error("expected bypass to fail after removing superadmin from bypass roles")
	}
}

func TestRBAC_UserDirectPermissions(t *testing.T) {
	manager := NewManager(nil)

	// User 42 has no permissions initially
	if manager.HasPermission(42, "read") {
		t.Error("expected user 42 to not have read permission initially")
	}

	// Assign permission directly to user 42
	manager.AssignPermissionToUser(42, "read")
	if !manager.HasPermission(42, "read") {
		t.Error("expected user 42 to have direct permission read")
	}

	// Remove permission directly from user 42
	manager.RemovePermissionFromUser(42, "read")
	if manager.HasPermission(42, "read") {
		t.Error("expected user 42 to not have read permission after removal")
	}
}

func TestRBAC_ContextualRules(t *testing.T) {
	manager := NewManager(nil)

	post1 := &mockPost{ID: 101, AuthorID: 5}
	post2 := &mockPost{ID: 102, AuthorID: 6}

	// Define contextual rule for edit-post
	manager.DefineRule("edit-post", func(userID any, context ...any) bool {
		if len(context) == 0 {
			return false
		}
		post, ok := context[0].(*mockPost)
		if !ok {
			return false
		}
		// User can edit if they are the author
		return post.AuthorID == userID
	})

	// User 5 should be able to edit post1
	if !manager.HasPermission(5, "edit-post", post1) {
		t.Error("expected author (user 5) to edit post1")
	}

	// User 5 should NOT be able to edit post2 (author is 6)
	if manager.HasPermission(5, "edit-post", post2) {
		t.Error("expected non-author (user 5) to be blocked from editing post2")
	}

	// Verify SuperAdmin bypasses the rule evaluation completely
	manager.AddBypassRole("superadmin")
	manager.AssignRole(99, "superadmin")
	if !manager.HasPermission(99, "edit-post", post2) {
		t.Error("expected superadmin to bypass edit-post rule and return true")
	}
}
