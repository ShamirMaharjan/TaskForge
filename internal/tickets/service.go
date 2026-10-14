package tickets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ShamirMaharjan/TaskForge/internal/audit"
	"github.com/ShamirMaharjan/TaskForge/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrEmptyRequest      = errors.New("ticket request description cannot be empty")
	ErrRequestTooLong    = errors.New("ticket request description exceeded maximum length of 500 characters")
	ErrInvalidDepartment = errors.New("target department does not exist")
	ErrReporterNotFound  = errors.New("reporter account does not exist or is inactive")
	ErrInvalidCategory   = errors.New("invalid category id provided")
	ErrInvalidPriority   = errors.New("invalid priority id provided")
)

type Ticket struct {
	ID              uuid.UUID
	Request         string
	ReporterID      uuid.UUID
	AssigneeID      *uuid.UUID
	TagDepartmentID uuid.UUID
	StatusID        uuid.UUID
	CategoryID      uuid.UUID
	PriorityID      uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateInput struct {
	// ReporterID must be populated from the authenticated token by the HTTP
	// handler, never from a request body field.
	ReporterID      uuid.UUID
	Request         string
	TagDepartmentID uuid.UUID
	CategoryID      *uuid.UUID
	PriorityID      *uuid.UUID
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*Ticket, error) {
	// 1. Domain validation
	reqClean := strings.TrimSpace(input.Request)

	if reqClean == "" {
		return nil, ErrEmptyRequest
	}

	if utf8.RuneCountInString(reqClean) > 500 {
		return nil, ErrRequestTooLong
	}

	var createdTicket Ticket

	//2. atomic change + audit transaction
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		//validate reporter exists and is active
		var isReporterEligible bool
		err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND status = 'active' AND email_verified = TRUE)", input.ReporterID).Scan(&isReporterEligible)
		if err != nil {
			return fmt.Errorf("check reporter status: %w", err)
		}

		if !isReporterEligible {
			return ErrReporterNotFound
		}

		//Validate Tag Department exists
		var deptExists bool

		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM departments WHERE id = $1)", input.TagDepartmentID).Scan(&deptExists)

		if err != nil {
			return fmt.Errorf("check department: %w", err)
		}

		if !deptExists {
			return ErrInvalidDepartment
		}

		// resolve unassigned Status Id
		var statusID uuid.UUID

		err = tx.QueryRow(ctx, "SELECT id FROM statuses WHERE slug = 'unassigned' ").Scan(&statusID)

		if err != nil {
			return fmt.Errorf("system default status 'unassigned' missing: %w", err)
		}

		// resolve category id (default to 'general' slug in nil)
		categoryID := input.CategoryID
		if categoryID == nil {
			var defaultCatID uuid.UUID
			err = tx.QueryRow(ctx, "SELECT id FROM categories WHERE slug = 'general' ").Scan(&defaultCatID)
			if err != nil {
				return fmt.Errorf("system default category 'general' missing: %w", err)
			}
			categoryID = &defaultCatID
		} else {
			var catExists bool
			err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM categories WHERE id = $1)", *categoryID).Scan(&catExists)
			if err != nil || !catExists {
				if err != nil {
					return fmt.Errorf("check category: %w", err)
				}
				return ErrInvalidCategory
			}
		}

		// Resolve Priority ID (Default to 'medium' slug if nil)
		priorityID := input.PriorityID
		if priorityID == nil {
			var defaultPrioID uuid.UUID
			err = tx.QueryRow(ctx, "SELECT id FROM priorities WHERE slug = 'medium'").Scan(&defaultPrioID)
			if err != nil {
				return fmt.Errorf("system default priority 'medium' missing: %w", err)
			}
			priorityID = &defaultPrioID
		} else {
			var prioExists bool
			err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM priorities WHERE id = $1)", *priorityID).Scan(&prioExists)
			if err != nil || !prioExists {
				if err != nil {
					return fmt.Errorf("check priority: %w", err)
				}
				return ErrInvalidPriority
			}
		}

		//insert ticket
		err = tx.QueryRow(ctx, `
				INSERT INTO tickets (request, reporter_id, tag_department_id, status_id, category_id, priority_id)
				VALUES ($1, $2, $3, $4, $5, $6)
				RETURNING id, request, reporter_id, assignee_id, tag_department_id, status_id, category_id, priority_id, created_at, updated_at`,
			reqClean, input.ReporterID, input.TagDepartmentID, statusID, *categoryID, *priorityID,
		).Scan(
			&createdTicket.ID,
			&createdTicket.Request,
			&createdTicket.ReporterID,
			&createdTicket.AssigneeID,
			&createdTicket.TagDepartmentID,
			&createdTicket.StatusID,
			&createdTicket.CategoryID,
			&createdTicket.PriorityID,
			&createdTicket.CreatedAt,
			&createdTicket.UpdatedAt,
		)

		if err != nil {
			return fmt.Errorf("insert ticket: %w", err)
		}
		// Append-Only Audit Entry
		return audit.Log(ctx, tx, audit.Entry{
			ActorID:    input.ReporterID,
			Action:     "ticket.created",
			EntityType: "ticket",
			EntityID:   createdTicket.ID,
			TicketID:   &createdTicket.ID,
			NewValue: map[string]any{
				"request":           createdTicket.Request,
				"tag_department_id": createdTicket.TagDepartmentID,
				"category_id":       createdTicket.CategoryID,
				"priority_id":       createdTicket.PriorityID,
			},
		})
	})

	if err != nil {
		return nil, err
	}

	return &createdTicket, nil
}
