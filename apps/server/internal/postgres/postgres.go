// Package postgres implements every domain Store on one connection pool.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"discordo/server/internal/errs"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ db *pgxpool.Pool }

// Open connects and applies pending migrations.
func Open(ctx context.Context, url string) (*Store, error) {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db}, nil
}

func (s *Store) Close() { s.db.Close() }

// migrate applies the embedded .sql files in name order, once each. Forward-only.
func migrate(ctx context.Context, db *pgxpool.Pool) error {
	names, err := fs.Glob(migrations, "migrations/*.sql") // sorted
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// The lock keeps two instances starting together from migrating twice.
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(7331)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `create table if not exists schema_migrations (name text primary key)`); err != nil {
			return err
		}
		for _, name := range names {
			var done bool
			if err := tx.QueryRow(ctx, `select exists(select 1 from schema_migrations where name = $1)`, name).Scan(&done); err != nil {
				return err
			}
			if done {
				continue
			}
			sql, err := migrations.ReadFile(name)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if _, err := tx.Exec(ctx, `insert into schema_migrations values ($1)`, name); err != nil {
				return err
			}
		}
		return nil
	})
}

// mapErr turns driver errors into domain errors.
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505": // unique_violation
			return errs.ErrConflict
		case "23503": // foreign_key_violation: something referenced does not exist
			return errs.ErrNotFound
		}
	}
	return err
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func exec(ctx context.Context, db execer, sql string, args ...any) error {
	_, err := db.Exec(ctx, sql, args...)
	return mapErr(err)
}

// execOne is exec for statements that must hit a row.
func execOne(ctx context.Context, db execer, sql string, args ...any) error {
	tag, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return errs.ErrNotFound
	}
	return nil
}
