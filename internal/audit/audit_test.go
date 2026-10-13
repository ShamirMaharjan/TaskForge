package audit_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ShamirMaharjan/TaskForge/internal/audit"
	"github.com/ShamirMaharjan/TaskForge/internal/db"
)

type testDB struct {
	AppPool   *pgxpool.Pool
	OwnerPool *pgxpool.Pool
}

func setupTestDatabase(t *testing.T) *testDB {
	t.Helper()

	ownerBaseURL := os.Getenv("TASKFORGE_TEST_OWNER_URL")
	if ownerBaseURL == "" {
		t.Skip("Skipping integration test: TASKFORGE_TEST_OWNER_URL not set")
	}
	appPass := os.Getenv("TASKFORGE_APP_DB_PASS")
	if appPass == "" {
		t.Skip("Skipping integration test: TASKFORGE_APP_DB_PASS not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Connect as superuser to default postgres DB
	ownerConfig, err := pgx.ParseConfig(ownerBaseURL)
	if err != nil {
		t.Fatalf("Failed to parse owner URL: %v", err)
	}
	adminPool, err := pgxpool.New(ctx, ownerBaseURL)
	if err != nil {
		t.Fatalf("Failed to connect as owner: %v", err)
	}

	var roleExists bool
	if err := adminPool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taskforge_app')").Scan(&roleExists); err != nil {
		adminPool.Close()
		t.Fatalf("Failed to check taskforge_app role: %v", err)
	}
	if !roleExists {
		var createRoleSQL string
		if err := adminPool.QueryRow(ctx, "SELECT format('CREATE ROLE taskforge_app WITH LOGIN PASSWORD %L', $1)", appPass).Scan(&createRoleSQL); err != nil {
			adminPool.Close()
			t.Fatalf("Failed to prepare taskforge_app role creation: %v", err)
		}
		if _, err := adminPool.Exec(ctx, createRoleSQL); err != nil {
			adminPool.Close()
			t.Fatalf("Failed to create taskforge_app role: %v", err)
		}
	}

	// 2. Create throwaway test database
	dbName := fmt.Sprintf("taskforge_test_%s", uuid.New().String()[:8])
	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s OWNER postgres", dbName))
	if err != nil {
		t.Fatalf("Failed to create test DB %s: %v", dbName, err)
	}

	// Dynamic URLs
	ownerConfig.Database = dbName
	testOwnerURL := ownerConfig.ConnString()
	appConfig := *ownerConfig
	appConfig.User = "taskforge_app"
	appConfig.Password = appPass
	testAppURL := appConfig.ConnString()

	// Register teardown cleanup
	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Terminate active connections and drop test database
		if _, err := adminPool.Exec(cleanupCtx, fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'", dbName)); err != nil {
			t.Errorf("Failed to terminate test database connections: %v", err)
		}
		if _, err := adminPool.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName)); err != nil {
			t.Errorf("Failed to drop test database %s: %v", dbName, err)
		}
		adminPool.Close()
	})

	// 3. Run migrations on throwaway DB
	migrationsPath, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("Failed to resolve migrations path: %v", err)
	}

	ownerPool, err := pgxpool.New(ctx, testOwnerURL)
	if err != nil {
		t.Fatalf("Failed to connect owner pool to %s: %v", dbName, err)
	}

	migrationFiles, err := filepath.Glob(filepath.Join(migrationsPath, "*.up.sql"))
	if err != nil {
		ownerPool.Close()
		t.Fatalf("Failed to find migration files: %v", err)
	}
	if len(migrationFiles) == 0 {
		ownerPool.Close()
		t.Fatalf("No migration files found in %s", migrationsPath)
	}
	for _, migrationFile := range migrationFiles {
		migrationSQL, err := os.ReadFile(migrationFile)
		if err != nil {
			ownerPool.Close()
			t.Fatalf("Failed to read migration %s: %v", migrationFile, err)
		}
		if _, err := ownerPool.Exec(ctx, string(migrationSQL)); err != nil {
			ownerPool.Close()
			t.Fatalf("Migration %s failed: %v", migrationFile, err)
		}
	}

	// 4. Apply permissions script
	grantsPath, err := filepath.Abs("../../scripts/02_grant_permissions.sql")
	if err != nil {
		t.Fatalf("Failed to resolve grants script path: %v", err)
	}
	grantsSQL, err := os.ReadFile(grantsPath)
	if err != nil {
		t.Fatalf("Failed to read grants script: %v", err)
	}

	_, err = ownerPool.Exec(ctx, string(grantsSQL))
	if err != nil {
		ownerPool.Close()
		t.Fatalf("Failed to apply grants: %v", err)
	}

	// 5. Connect app pool as taskforge_app
	appPool, err := pgxpool.New(ctx, testAppURL)
	if err != nil {
		ownerPool.Close()
		t.Fatalf("Failed to connect app pool as taskforge_app: %v", err)
	}

	return &testDB{
		AppPool:   appPool,
		OwnerPool: ownerPool,
	}
}

func TestTransactionalTicketAndAudit(t *testing.T) {
	tdb := setupTestDatabase(t)
	defer tdb.AppPool.Close()
	defer tdb.OwnerPool.Close()

	ctx := context.Background()

	// Fetch lookup IDs from seeded migration tables
	var reporterID, deptID, statusID, categoryID, priorityID uuid.UUID
	err := tdb.AppPool.QueryRow(ctx, "SELECT id FROM users WHERE is_system = TRUE").Scan(&reporterID)
	if err != nil {
		t.Fatalf("Failed to fetch system user: %v", err)
	}
	err = tdb.AppPool.QueryRow(ctx, "SELECT id FROM departments LIMIT 1").Scan(&deptID)
	if err != nil {
		t.Fatalf("Failed to fetch department: %v", err)
	}
	err = tdb.AppPool.QueryRow(ctx, "SELECT id FROM statuses WHERE slug = 'unassigned'").Scan(&statusID)
	if err != nil {
		t.Fatalf("Failed to fetch status: %v", err)
	}
	err = tdb.AppPool.QueryRow(ctx, "SELECT id FROM categories LIMIT 1").Scan(&categoryID)
	if err != nil {
		t.Fatalf("Failed to fetch category: %v", err)
	}
	err = tdb.AppPool.QueryRow(ctx, "SELECT id FROM priorities LIMIT 1").Scan(&priorityID)
	if err != nil {
		t.Fatalf("Failed to fetch priority: %v", err)
	}

	t.Run("Commit Path: Ticket and Audit Present", func(t *testing.T) {
		ticketID := uuid.New()

		err := db.WithTx(ctx, tdb.AppPool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO tickets (id, request, reporter_id, tag_department_id, status_id, category_id, priority_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				ticketID, "Server down in Rack 4", reporterID, deptID, statusID, categoryID, priorityID,
			)
			if err != nil {
				return err
			}

			return audit.Log(ctx, tx, audit.Entry{
				ActorID:    reporterID,
				Action:     "ticket.created",
				EntityType: "ticket",
				EntityID:   ticketID,
				TicketID:   &ticketID,
				NewValue:   map[string]string{"request": "Server down in Rack 4"},
			})
		})
		if err != nil {
			t.Fatalf("Transaction failed: %v", err)
		}

		// Verify ticket exists
		var req string
		err = tdb.AppPool.QueryRow(ctx, "SELECT request FROM tickets WHERE id = $1", ticketID).Scan(&req)
		if err != nil {
			t.Errorf("Expected ticket to exist after commit: %v", err)
		}

		// Verify audit entry exists
		var action string
		err = tdb.AppPool.QueryRow(ctx, "SELECT action FROM audit WHERE ticket_id = $1", ticketID).Scan(&action)
		if err != nil {
			t.Errorf("Expected audit row to exist after commit: %v", err)
		}
	})

	t.Run("Rollback Path: Forced Error Before Commit", func(t *testing.T) {
		ticketID := uuid.New()

		err := db.WithTx(ctx, tdb.AppPool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO tickets (id, request, reporter_id, tag_department_id, status_id, category_id, priority_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				ticketID, "Failed transaction ticket", reporterID, deptID, statusID, categoryID, priorityID,
			)
			if err != nil {
				return err
			}

			if err := audit.Log(ctx, tx, audit.Entry{
				ActorID:    reporterID,
				Action:     "ticket.created",
				EntityType: "ticket",
				EntityID:   ticketID,
				TicketID:   &ticketID,
			}); err != nil {
				return fmt.Errorf("write audit row: %w", err)
			}

			// Force transaction error
			return fmt.Errorf("simulated business error before commit")
		})

		if err == nil {
			t.Fatalf("Expected error from transaction execution, got nil")
		}

		// Verify ticket does NOT exist
		var count int
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("Failed to check rolled-back ticket: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected ticket to be rolled back, found %d rows", count)
		}

		// Verify audit entry does NOT exist
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE ticket_id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("Failed to check rolled-back audit row: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected audit row to be rolled back, found %d rows", count)
		}
	})

	t.Run("Rollback Path: Panic", func(t *testing.T) {
		ticketID := uuid.New()
		didPanic := false

		func() {
			defer func() {
				didPanic = recover() != nil
			}()
			_ = db.WithTx(ctx, tdb.AppPool, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO tickets (id, request, reporter_id, tag_department_id, status_id, category_id, priority_id)
					VALUES ($1, $2, $3, $4, $5, $6, $7)`,
					ticketID, "Panicked transaction ticket", reporterID, deptID, statusID, categoryID, priorityID,
				)
				if err != nil {
					return err
				}
				if err := audit.Log(ctx, tx, audit.Entry{
					ActorID: reporterID, Action: "ticket.created", EntityType: "ticket",
					EntityID: ticketID, TicketID: &ticketID,
				}); err != nil {
					return err
				}
				panic("simulated panic")
			})
		}()

		if !didPanic {
			t.Fatal("Expected transaction callback panic to propagate")
		}
		var count int
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("Failed to check panicked transaction: %v", err)
		}
		if count != 0 {
			t.Fatalf("Expected panicked transaction to roll back, found %d ticket rows", count)
		}
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE ticket_id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("Failed to check panicked audit row: %v", err)
		}
		if count != 0 {
			t.Fatalf("Expected panicked transaction to roll back, found %d audit rows", count)
		}
	})

	t.Run("Rollback Path: Cancelled Context", func(t *testing.T) {
		ticketID := uuid.New()
		cancelCtx, cancel := context.WithCancel(ctx)
		err := db.WithTx(cancelCtx, tdb.AppPool, func(tx pgx.Tx) error {
			_, err := tx.Exec(cancelCtx, `
				INSERT INTO tickets (id, request, reporter_id, tag_department_id, status_id, category_id, priority_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				ticketID, "Cancelled transaction ticket", reporterID, deptID, statusID, categoryID, priorityID,
			)
			if err != nil {
				return err
			}
			if err := audit.Log(cancelCtx, tx, audit.Entry{
				ActorID: reporterID, Action: "ticket.created", EntityType: "ticket",
				EntityID: ticketID, TicketID: &ticketID,
			}); err != nil {
				return err
			}
			cancel()
			return cancelCtx.Err()
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Expected context cancellation error, got %v", err)
		}
		var count int
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("Failed to check cancelled transaction: %v", err)
		}
		if count != 0 {
			t.Fatalf("Expected cancelled transaction to roll back, found %d ticket rows", count)
		}
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE ticket_id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("Failed to check cancelled audit row: %v", err)
		}
		if count != 0 {
			t.Fatalf("Expected cancelled transaction to roll back, found %d audit rows", count)
		}
	})

	t.Run("RBAC: App Role Insert Allowed, Update Forbidden", func(t *testing.T) {
		auditID := uuid.New()

		// 1. Direct INSERT into audit using taskforge_app must succeed
		_, err := tdb.AppPool.Exec(ctx, `
			INSERT INTO audit (id, actor_id, action, entity_type, entity_id)
			VALUES ($1, $2, $3, $4, $5)`,
			auditID, reporterID, "system.test", "system", reporterID,
		)
		if err != nil {
			t.Fatalf("INSERT into audit should be granted to taskforge_app: %v", err)
		}

		// 2. Direct UPDATE on audit using taskforge_app must fail due to trigger/grant restriction
		_, err = tdb.AppPool.Exec(ctx, "UPDATE audit SET action = 'tampered' WHERE id = $1", auditID)
		if err == nil {
			t.Fatalf("Expected UPDATE on audit to fail, but it succeeded")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("Expected PostgreSQL insufficient-privilege error (42501), got %v", err)
		}

		// The owner bypasses the grant restriction, so this separately proves the trigger.
		_, err = tdb.OwnerPool.Exec(ctx, "UPDATE audit SET action = 'tampered' WHERE id = $1", auditID)
		if err == nil {
			t.Fatalf("Expected owner UPDATE on audit to fail, but it succeeded")
		}
		if !errors.As(err, &pgErr) || pgErr.Code != "P0001" {
			t.Fatalf("Expected audit trigger exception (P0001), got %v", err)
		}
	})
}
