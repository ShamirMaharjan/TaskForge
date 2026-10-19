//go:build integration

package audit_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ShamirMaharjan/TaskForge/internal/audit"
	"github.com/ShamirMaharjan/TaskForge/internal/db"
	"github.com/ShamirMaharjan/TaskForge/internal/testutil"
)

func TestTransactionalTicketAndAudit(t *testing.T) {
	tdb := testutil.SetupTestDatabase(t)
	defer tdb.AppPool.Close()
	defer tdb.OwnerPool.Close()

	ctx := context.Background()
	var reporterID, deptID, statusID, categoryID, priorityID uuid.UUID
	for query, destination := range map[string]any{
		"SELECT id FROM users WHERE is_system = TRUE":       &reporterID,
		"SELECT id FROM departments LIMIT 1":                &deptID,
		"SELECT id FROM statuses WHERE slug = 'unassigned'": &statusID,
		"SELECT id FROM categories LIMIT 1":                 &categoryID,
		"SELECT id FROM priorities LIMIT 1":                 &priorityID,
	} {
		if err := tdb.AppPool.QueryRow(ctx, query).Scan(destination); err != nil {
			t.Fatalf("failed to fetch fixture for %q: %v", query, err)
		}
	}

	t.Run("commit path", func(t *testing.T) {
		ticketID := uuid.New()
		err := db.WithTx(ctx, tdb.AppPool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO tickets (id, request, reporter_id, tag_department_id, status_id, category_id, priority_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				ticketID, "Server down in Rack 4", reporterID, deptID, statusID, categoryID, priorityID); err != nil {
				return err
			}
			return audit.Log(ctx, tx, audit.Entry{
				ActorID: reporterID, Action: "ticket.created", EntityType: "ticket",
				EntityID: ticketID, TicketID: &ticketID,
			})
		})
		if err != nil {
			t.Fatalf("transaction failed: %v", err)
		}

		var count int
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("failed to check committed ticket: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected one committed ticket, got %d", count)
		}
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE ticket_id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("failed to check committed audit row: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected one committed audit row, got %d", count)
		}
	})

	t.Run("rollback path", func(t *testing.T) {
		ticketID := uuid.New()
		err := db.WithTx(ctx, tdb.AppPool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO tickets (id, request, reporter_id, tag_department_id, status_id, category_id, priority_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				ticketID, "Failed transaction ticket", reporterID, deptID, statusID, categoryID, priorityID); err != nil {
				return err
			}
			if err := audit.Log(ctx, tx, audit.Entry{ActorID: reporterID, Action: "ticket.created", EntityType: "ticket", EntityID: ticketID, TicketID: &ticketID}); err != nil {
				return fmt.Errorf("write audit row: %w", err)
			}
			return errors.New("simulated business error")
		})
		if err == nil {
			t.Fatal("expected transaction error")
		}

		var count int
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("failed to check rolled-back ticket: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no rolled-back ticket, got %d", count)
		}
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE ticket_id = $1", ticketID).Scan(&count); err != nil {
			t.Fatalf("failed to check rolled-back audit row: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no rolled-back audit row, got %d", count)
		}
	})

	t.Run("panic path", func(t *testing.T) {
		ticketID := uuid.New()
		didPanic := false
		func() {
			defer func() { didPanic = recover() != nil }()
			_ = db.WithTx(ctx, tdb.AppPool, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `
					INSERT INTO tickets (id, request, reporter_id, tag_department_id, status_id, category_id, priority_id)
					VALUES ($1, $2, $3, $4, $5, $6, $7)`,
					ticketID, "Panicked transaction ticket", reporterID, deptID, statusID, categoryID, priorityID); err != nil {
					return err
				}
				if err := audit.Log(ctx, tx, audit.Entry{ActorID: reporterID, Action: "ticket.created", EntityType: "ticket", EntityID: ticketID, TicketID: &ticketID}); err != nil {
					return err
				}
				panic("simulated panic")
			})
		}()
		if !didPanic {
			t.Fatal("expected transaction callback panic to propagate")
		}
		var count int
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE id = $1", ticketID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected panicked ticket to roll back, got %d", count)
		}
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE ticket_id = $1", ticketID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected panicked audit row to roll back, got %d", count)
		}
	})

	t.Run("cancelled context path", func(t *testing.T) {
		ticketID := uuid.New()
		cancelCtx, cancel := context.WithCancel(ctx)
		err := db.WithTx(cancelCtx, tdb.AppPool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(cancelCtx, `
				INSERT INTO tickets (id, request, reporter_id, tag_department_id, status_id, category_id, priority_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				ticketID, "Cancelled transaction ticket", reporterID, deptID, statusID, categoryID, priorityID); err != nil {
				return err
			}
			if err := audit.Log(cancelCtx, tx, audit.Entry{ActorID: reporterID, Action: "ticket.created", EntityType: "ticket", EntityID: ticketID, TicketID: &ticketID}); err != nil {
				return err
			}
			cancel()
			return cancelCtx.Err()
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
		if err != context.Canceled {
			t.Fatalf("expected the original cancellation error without rollback wrapping, got %v", err)
		}
		var count int
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM tickets WHERE id = $1", ticketID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected cancelled ticket to roll back, got %d", count)
		}
		if err := tdb.AppPool.QueryRow(ctx, "SELECT COUNT(*) FROM audit WHERE ticket_id = $1", ticketID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected cancelled audit row to roll back, got %d", count)
		}
	})

	t.Run("RBAC", func(t *testing.T) {
		auditID := uuid.New()
		if _, err := tdb.AppPool.Exec(ctx, `
			INSERT INTO audit (id, actor_id, action, entity_type, entity_id)
			VALUES ($1, $2, $3, $4, $5)`, auditID, reporterID, "system.test", "system", reporterID); err != nil {
			t.Fatalf("audit INSERT should be granted: %v", err)
		}
		_, err := tdb.AppPool.Exec(ctx, "UPDATE audit SET action = 'tampered' WHERE id = $1", auditID)
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("expected insufficient privilege on audit UPDATE, got %v", err)
		}
		_, err = tdb.OwnerPool.Exec(ctx, "UPDATE audit SET action = 'tampered' WHERE id = $1", auditID)
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "P0001" {
			t.Fatalf("expected audit immutability trigger error, got %v", err)
		}
	})
}
