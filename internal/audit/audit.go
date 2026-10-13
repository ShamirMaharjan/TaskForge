package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type Entry struct {
	ActorID    uuid.UUID
	Action     string
	EntityType string
	EntityID   uuid.UUID
	TicketID   *uuid.UUID
	OldValue   any
	NewValue   any
}

func Log(ctx context.Context, execer execer, entry Entry) error {
	var oldJson, newJson []byte
	var err error

	if entry.OldValue != nil {
		oldJson, err = json.Marshal(entry.OldValue)
		if err != nil {
			return fmt.Errorf("marshal old value: %w", err)
		}
	}

	if entry.NewValue != nil {
		newJson, err = json.Marshal(entry.NewValue)

		if err != nil {
			return fmt.Errorf("marshal new value: %w", err)
		}
	}

	_, err = execer.Exec(ctx, `
	INSERT INTO audit (actor_id, action, entity_type, entity_id, ticket_id, old_value, new_value)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.ActorID,
		entry.Action,
		entry.EntityType,
		entry.EntityID,
		entry.TicketID,
		oldJson,
		newJson,
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}

	return nil
}
