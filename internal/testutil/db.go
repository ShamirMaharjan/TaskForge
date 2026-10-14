package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	OwnerURLEnv        = "TASKFORGE_TEST_OWNER_URL"
	AppPassEnv         = "TASKFORGE_APP_DB_PASS"
	RequireIntegration = "TASKFORGE_REQUIRE_INTEGRATION"
)

type TestDB struct {
	AppPool   *pgxpool.Pool
	OwnerPool *pgxpool.Pool
}

type UserOptions struct {
	Status        string
	EmailVerified bool
}

func InsertUser(t *testing.T, tdb *TestDB, options UserOptions) uuid.UUID {
	t.Helper()

	ctx := context.Background()
	userID := uuid.New()
	username := "test-" + strings.ReplaceAll(userID.String(), "-", "")
	email := username + "@example.test"

	var departmentID, roleID uuid.UUID
	if err := tdb.OwnerPool.QueryRow(ctx, "SELECT id FROM departments WHERE name = 'IT'").Scan(&departmentID); err != nil {
		t.Fatalf("failed to find IT department: %v", err)
	}
	if err := tdb.OwnerPool.QueryRow(ctx, "SELECT id FROM roles WHERE slug = 'employee'").Scan(&roleID); err != nil {
		t.Fatalf("failed to find employee role: %v", err)
	}

	_, err := tdb.OwnerPool.Exec(ctx, `
		INSERT INTO users (id, username, email, full_name, department_id, role_id, status, email_verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userID, username, email, "Test User", departmentID, roleID, options.Status, options.EmailVerified)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}

	return userID
}

func SetupTestDatabase(t *testing.T) *TestDB {
	t.Helper()

	ownerBaseURL := os.Getenv(OwnerURLEnv)

	if ownerBaseURL == "" {
		if os.Getenv(RequireIntegration) == "1" {
			t.Fatalf("integration testing required but %s is not set", OwnerURLEnv)
		}
		t.Skipf("Skipping integration testing: %s not set", OwnerURLEnv)
	}

	appPass := os.Getenv(AppPassEnv)

	if appPass == "" {
		if os.Getenv(RequireIntegration) == "1" {
			t.Fatalf("integration testing required but %s is not set", AppPassEnv)
		}
		t.Skipf("Skipping integration testing: %s not set", AppPassEnv)
	}

	parsedURL, err := url.Parse(ownerBaseURL)

	if err != nil {
		t.Fatalf("Invalid %s: %v", OwnerURLEnv, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

	defer cancel()

	adminPool, err := pgxpool.New(ctx, ownerBaseURL)

	if err != nil {
		t.Fatalf("Failed to connect as owner: %v", err)
	}

	var roleExists bool

	err = adminPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = 'taskforge_app')").Scan(&roleExists)

	if err != nil || !roleExists {
		adminPool.Close()
		t.Fatalf("Prerequisite failed: role 'taskforge_app' missing. Run scripts/01_init_roles.sh first.")
	}

	dbName := fmt.Sprintf("taskforge_test_%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:12])
	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s OWNER postgres;", dbName))
	if err != nil {
		adminPool.Close()
		t.Fatalf("Failed to create test DB %s: %v", dbName, err)
	}

	ownerTestURLParsed := *parsedURL
	ownerTestURLParsed.Path = "/" + dbName
	testOwnerURL := ownerTestURLParsed.String()

	appTestURLParsed := *parsedURL
	appTestURLParsed.User = url.UserPassword("taskforge_app", appPass)
	appTestURLParsed.Path = "/" + dbName
	testAppURL := appTestURLParsed.String()

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		if _, err := adminPool.Exec(cleanupCtx, fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s';", dbName)); err != nil {
			t.Errorf("Failed to terminate test database connections: %v", err)
		}
		if _, err := adminPool.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbName)); err != nil {
			t.Errorf("Failed to drop test database %s: %v", dbName, err)
		}
		adminPool.Close()
	})

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to locate testutil source file")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	migrationsPath := filepath.Join(repoRoot, "migrations")
	grantsPath := filepath.Join(repoRoot, "scripts", "02_grant_permissions.sql")
	if _, err := os.Stat(migrationsPath); err != nil {
		t.Fatalf("Failed to locate migrations directory: %v", err)
	}
	if _, err := os.Stat(grantsPath); err != nil {
		t.Fatalf("Failed to locate grants script: %v", err)
	}

	migrationsURL := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(migrationsPath),
	}
	m, err := migrate.New(migrationsURL.String(), testOwnerURL)
	if err != nil {
		t.Fatalf("Migration initialization failed: %v", err)
	}
	if err := m.Up(); err != nil {
		sourceErr, dbErr := m.Close()
		t.Fatalf("Migration up failed: %v (close errors: source=%v database=%v)", err, sourceErr, dbErr)
	}
	if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
		t.Fatalf("Migration close failed: source=%v database=%v", sourceErr, dbErr)
	}

	grantsSQL, err := os.ReadFile(grantsPath)
	if err != nil {
		t.Fatalf("Failed to read grants script: %v", err)
	}

	ownerPool, err := pgxpool.New(ctx, testOwnerURL)
	if err != nil {
		t.Fatalf("Failed to connect owner pool to %s: %v", dbName, err)
	}

	_, err = ownerPool.Exec(ctx, string(grantsSQL))
	if err != nil {
		ownerPool.Close()
		t.Fatalf("Failed to apply grants: %v", err)
	}

	appPool, err := pgxpool.New(ctx, testAppURL)
	if err != nil {
		ownerPool.Close()
		t.Fatalf("Failed to connect app pool as taskforge_app: %v", err)
	}

	return &TestDB{
		AppPool:   appPool,
		OwnerPool: ownerPool,
	}
}
