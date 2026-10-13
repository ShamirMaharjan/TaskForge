package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)

	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			rollbackCtx := context.WithoutCancel(ctx)
			_ = tx.Rollback(rollbackCtx)
			panic(p)
		} else if err != nil {
			rollbackCtx := context.WithoutCancel(ctx)
			if rbErr := tx.Rollback(rollbackCtx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = fmt.Errorf("transaction error: %v (rollback error: %w)", err, rbErr)
			}
		}
	}()

	err = fn(tx)
	if err != nil {
		return err
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return fmt.Errorf("commit transaction: %w", commitErr)
	}

	return nil
}
