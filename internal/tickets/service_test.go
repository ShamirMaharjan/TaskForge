//go:build integration

package tickets_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ShamirMaharjan/TaskForge/internal/testutil"
	"github.com/ShamirMaharjan/TaskForge/internal/tickets"
)

func fixture(t *testing.T) (*testutil.TestDB, context.Context, uuid.UUID, uuid.UUID) {
	t.Helper()
	tdb := testutil.SetupTestDatabase(t)
	ctx := context.Background()
	reporterID := testutil.InsertUser(t, tdb, testutil.UserOptions{Status: "active", EmailVerified: true})
	var deptID uuid.UUID
	if err := tdb.AppPool.QueryRow(ctx, "SELECT id FROM departments LIMIT 1").Scan(&deptID); err != nil {
		t.Fatalf("failed to query department: %v", err)
	}
	return tdb, ctx, reporterID, deptID
}

func closeDB(t *testing.T, tdb *testutil.TestDB) {
	t.Helper()
	tdb.AppPool.Close()
	tdb.OwnerPool.Close()
}

func TestTicketCreation(t *testing.T) {
	tdb, ctx, reporterID, deptID := fixture(t)
	defer closeDB(t, tdb)
	svc := tickets.NewService(tdb.AppPool)

	var customCatID, customPrioID uuid.UUID
	if err := tdb.AppPool.QueryRow(ctx, "SELECT id FROM categories WHERE slug = 'hardware'").Scan(&customCatID); err != nil {
		t.Fatalf("failed to query category: %v", err)
	}
	if err := tdb.AppPool.QueryRow(ctx, "SELECT id FROM priorities WHERE slug = 'urgent'").Scan(&customPrioID); err != nil {
		t.Fatalf("failed to query priority: %v", err)
	}

	t.Run("defaults and audit", func(t *testing.T) {
		ticket, err := svc.Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: "Monitors flicker on 3rd floor"})
		if err != nil {
			t.Fatalf("expected creation to succeed: %v", err)
		}
		var expectedCategory, expectedPriority, expectedStatus uuid.UUID
		if err := tdb.AppPool.QueryRow(ctx, "SELECT id FROM categories WHERE slug = 'general'").Scan(&expectedCategory); err != nil {
			t.Fatal(err)
		}
		if err := tdb.AppPool.QueryRow(ctx, "SELECT id FROM priorities WHERE slug = 'medium'").Scan(&expectedPriority); err != nil {
			t.Fatal(err)
		}
		if err := tdb.AppPool.QueryRow(ctx, "SELECT id FROM statuses WHERE slug = 'unassigned'").Scan(&expectedStatus); err != nil {
			t.Fatal(err)
		}
		if ticket.CategoryID != expectedCategory || ticket.PriorityID != expectedPriority || ticket.StatusID != expectedStatus {
			t.Fatalf("unexpected defaults: status=%s category=%s priority=%s", ticket.StatusID, ticket.CategoryID, ticket.PriorityID)
		}
		var action string
		if err := tdb.AppPool.QueryRow(ctx, "SELECT action FROM audit WHERE ticket_id = $1", ticket.ID).Scan(&action); err != nil {
			t.Fatalf("expected audit record: %v", err)
		}
		if action != "ticket.created" {
			t.Fatalf("expected ticket.created, got %s", action)
		}
	})

	t.Run("explicit category and priority", func(t *testing.T) {
		ticket, err := svc.Create(ctx, tickets.CreateInput{
			ReporterID: reporterID, TagDepartmentID: deptID, Request: "Main gateway router failure",
			CategoryID: &customCatID, PriorityID: &customPrioID,
		})
		if err != nil {
			t.Fatalf("expected creation to succeed: %v", err)
		}
		if ticket.CategoryID != customCatID || ticket.PriorityID != customPrioID {
			t.Fatalf("unexpected explicit values: category=%s priority=%s", ticket.CategoryID, ticket.PriorityID)
		}
	})

	t.Run("domain validation", func(t *testing.T) {
		t.Run("empty", func(t *testing.T) {
			_, err := svc.Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: "   "})
			if !errors.Is(err, tickets.ErrEmptyRequest) {
				t.Errorf("expected ErrEmptyRequest, got %v", err)
			}
		})
		t.Run("500 characters", func(t *testing.T) {
			_, err := svc.Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: strings.Repeat("न", 500)})
			if err != nil {
				t.Fatalf("expected 500 characters to succeed: %v", err)
			}
		})
		t.Run("too long", func(t *testing.T) {
			_, err := svc.Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: strings.Repeat("🙂", 501)})
			if !errors.Is(err, tickets.ErrRequestTooLong) {
				t.Errorf("expected ErrRequestTooLong, got %v", err)
			}
		})
		t.Run("invalid department", func(t *testing.T) {
			_, err := svc.Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: uuid.New(), Request: "ghost department"})
			if !errors.Is(err, tickets.ErrInvalidDepartment) {
				t.Errorf("expected ErrInvalidDepartment, got %v", err)
			}
		})
		t.Run("invalid category and priority", func(t *testing.T) {
			fakeID := uuid.New()
			_, err := svc.Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: "bad category", CategoryID: &fakeID})
			if !errors.Is(err, tickets.ErrInvalidCategory) {
				t.Errorf("expected ErrInvalidCategory, got %v", err)
			}
			_, err = svc.Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: "bad priority", PriorityID: &fakeID})
			if !errors.Is(err, tickets.ErrInvalidPriority) {
				t.Errorf("expected ErrInvalidPriority, got %v", err)
			}
		})
	})

	t.Run("pending and unverified reporters", func(t *testing.T) {
		for name, options := range map[string]testutil.UserOptions{
			"pending":    {Status: "pending", EmailVerified: true},
			"unverified": {Status: "active", EmailVerified: false},
		} {
			t.Run(name, func(t *testing.T) {
				reporter := testutil.InsertUser(t, tdb, options)
				_, err := svc.Create(ctx, tickets.CreateInput{ReporterID: reporter, TagDepartmentID: deptID, Request: "rejected reporter"})
				if !errors.Is(err, tickets.ErrReporterNotFound) {
					t.Errorf("expected ErrReporterNotFound, got %v", err)
				}
			})
		}
	})
}

func TestTicketCreationRollsBackWhenAuditInsertFails(t *testing.T) {
	tdb, ctx, reporterID, deptID := fixture(t)
	t.Cleanup(func() { closeDB(t, tdb) })
	if _, err := tickets.NewService(tdb.AppPool).Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: "Audit success control"}); err != nil {
		t.Fatalf("expected control Create to succeed before revoke: %v", err)
	}
	var auditCountBefore int
	if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE action = 'ticket.created'").Scan(&auditCountBefore); err != nil {
		t.Fatalf("failed to count control audit rows: %v", err)
	}
	if _, err := tdb.OwnerPool.Exec(ctx, "REVOKE INSERT ON audit FROM taskforge_app"); err != nil {
		t.Fatalf("failed to revoke audit INSERT: %v", err)
	}
	_, err := tickets.NewService(tdb.AppPool).Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: "Audit failure rollback"})
	if err == nil {
		t.Fatal("expected Create to fail when audit INSERT is revoked")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("expected insufficient privilege error, got %v", err)
	}
	var count int
	if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE request = $1", "Audit failure rollback").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected zero tickets after audit failure, got %d", count)
	}
	if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE action = 'ticket.created'").Scan(&count); err != nil {
		t.Fatalf("failed to count audit rows after rollback: %v", err)
	}
	if count != auditCountBefore {
		t.Fatalf("expected audit count to remain %d after rollback, got %d", auditCountBefore, count)
	}
}

func TestTicketCreationMissingDefaultPriorityIsInfrastructureError(t *testing.T) {
	tdb, ctx, reporterID, deptID := fixture(t)
	defer closeDB(t, tdb)
	if _, err := tdb.OwnerPool.Exec(ctx, "DELETE FROM priorities WHERE slug = 'medium'"); err != nil {
		t.Fatalf("failed to remove medium priority: %v", err)
	}
	_, err := tickets.NewService(tdb.AppPool).Create(ctx, tickets.CreateInput{ReporterID: reporterID, TagDepartmentID: deptID, Request: "Missing default priority"})
	if err == nil {
		t.Fatal("expected missing default priority to fail")
	}
	for _, sentinel := range []error{tickets.ErrEmptyRequest, tickets.ErrRequestTooLong, tickets.ErrInvalidDepartment, tickets.ErrReporterNotFound, tickets.ErrInvalidCategory, tickets.ErrInvalidPriority} {
		if errors.Is(err, sentinel) {
			t.Errorf("expected infrastructure error, got domain sentinel %v: %v", sentinel, err)
		}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected missing priority to wrap pgx.ErrNoRows, got %v", err)
	}
}
