package internal

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/sqlite3"
	_ "modernc.org/sqlite"
)

type Repository struct {
	sql *sql.DB
	db  *goqu.Database
}

func OpenRepository(path string) (*Repository, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	u := url.URL{Scheme: "file", Path: absolutePath}
	values := u.Query()
	values.Add("_pragma", "foreign_keys(1)")
	values.Add("_pragma", "busy_timeout(5000)")
	values.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = values.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	r := &Repository{sql: db, db: goqu.New("sqlite3", db)}
	if err := r.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

func (r *Repository) Close() error { return r.sql.Close() }

func (r *Repository) migrate(ctx context.Context) error {
	tx, err := r.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 2 {
		return fmt.Errorf("database version %d is newer than supported version 2", version)
	}
	if version == 0 {
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
			return err
		}
	}
	if version < 2 {
		// language=sql
		if _, err := tx.ExecContext(ctx, `ALTER TABLE jobs ADD COLUMN cleanup_complete BOOLEAN NOT NULL DEFAULT FALSE CHECK (cleanup_complete IN (FALSE, TRUE))`); err != nil {
			return fmt.Errorf("add cleanup state: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type jobRow struct {
	ID                string                `db:"id"`
	URL               string                `db:"url"`
	Title             string                `db:"title"`
	DownloadType      string                `db:"download_type"`
	Options           JSON[DownloadOptions] `db:"options"`
	Status            string                `db:"status"`
	Outputs           JSON[[]OutputFile]    `db:"outputs"`
	Message           *string               `db:"message"`
	Error             *string               `db:"error"`
	Percent           *float64              `db:"percent"`
	DeleteRequestedAt *string               `db:"delete_requested_at"`
	DeleteOutputs     bool                  `db:"delete_outputs"`
	CleanupError      *string               `db:"cleanup_error"`
	CleanupAfter      *string               `db:"cleanup_after"`
	CleanupComplete   bool                  `db:"cleanup_complete"`
	CreatedAt         string                `db:"created_at"`
	FinishedAt        *string               `db:"finished_at"`
}

func (row jobRow) toDomain() Job {
	return Job{
		ID:                JobID(row.ID),
		URL:               row.URL,
		Title:             row.Title,
		DownloadType:      DownloadType(row.DownloadType),
		Options:           row.Options.Data,
		Status:            JobStatus(row.Status),
		Outputs:           row.Outputs.Data,
		Message:           row.Message,
		Error:             row.Error,
		Percent:           row.Percent,
		DeleteRequestedAt: row.DeleteRequestedAt,
		DeleteOutputs:     row.DeleteOutputs,
		CleanupError:      row.CleanupError,
		CleanupAfter:      row.CleanupAfter,
		CleanupComplete:   row.CleanupComplete,
		CreatedAt:         row.CreatedAt,
		FinishedAt:        row.FinishedAt,
	}
}

func (r *Repository) GetJob(ctx context.Context, id JobID) (Job, error) {
	q := r.db.From("jobs").
		Select(&jobRow{}).
		Where(goqu.C("id").Eq(id))
	var row jobRow
	found, err := q.ScanStructContext(ctx, &row)
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	if !found {
		return Job{}, sql.ErrNoRows
	}
	return row.toDomain(), nil
}

func (r *Repository) CreateJob(ctx context.Context, job Job) error {
	q := r.db.Insert("jobs").
		Rows(goqu.Record{
			"id":            job.ID,
			"url":           job.URL,
			"title":         job.Title,
			"download_type": job.DownloadType,
			"options":       JSON[DownloadOptions]{Data: job.Options},
		})
	if _, err := q.Executor().ExecContext(ctx); err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}
