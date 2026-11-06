package internal

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/doug-martin/goqu/v9"
)

func now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func (r *Repository) ListJobs(ctx context.Context, includeDeleted bool) ([]Job, error) {
	q := r.db.From("jobs").
		Select(&jobRow{}).
		Order(goqu.C("created_at").Asc(), goqu.C("id").Asc())
	if !includeDeleted {
		q = q.Where(goqu.C("delete_requested_at").IsNull())
	}
	var rows []jobRow
	if err := q.ScanStructsContext(ctx, &rows); err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	jobs := make([]Job, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.toDomain())
	}
	return jobs, nil
}

// ClaimJob only transitions a job that is still queued and has not been deleted.
func (r *Repository) ClaimJob(ctx context.Context, id JobID) (bool, error) {
	timestamp := now()
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"status":     JobRunning,
			"started_at": timestamp,
			"updated_at": timestamp,
		}).
		Where(
			goqu.C("id").Eq(id),
			goqu.C("status").Eq(JobQueued),
			goqu.C("delete_requested_at").IsNull(),
		)
	result, err := q.Executor().ExecContext(ctx)
	if err != nil {
		return false, fmt.Errorf("claim job: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

type FinishJobParams struct {
	ID      JobID
	Status  JobStatus
	Message *string
}

func (r *Repository) FinishJob(ctx context.Context, params FinishJobParams) error {
	timestamp := now()
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"status":      params.Status,
			"error":       params.Message,
			"updated_at":  timestamp,
			"finished_at": timestamp,
		}).
		Where(goqu.C("id").Eq(params.ID))
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) SetProgress(ctx context.Context, id JobID, percent float64) error {
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"percent":    percent,
			"updated_at": now(),
		}).
		Where(goqu.C("id").Eq(id), goqu.C("status").Eq(JobRunning))
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) SetTitle(ctx context.Context, id JobID, title string) error {
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"title":      title,
			"updated_at": now(),
		}).
		Where(goqu.C("id").Eq(id))
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) SetOutputs(ctx context.Context, id JobID, outputs []OutputFile) error {
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"outputs":    JSON[[]OutputFile]{Data: outputs},
			"updated_at": now(),
		}).
		Where(goqu.C("id").Eq(id))
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) RequestDelete(ctx context.Context, id JobID, deleteOutputs bool) (bool, error) {
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"delete_requested_at": now(),
			"delete_outputs":      deleteOutputs,
			"updated_at":          now(),
		}).
		Where(goqu.C("id").Eq(id))
	result, err := q.Executor().ExecContext(ctx)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (r *Repository) DeleteJob(ctx context.Context, id JobID) error {
	q := r.db.Delete("jobs").
		Where(goqu.C("id").Eq(id), goqu.C("delete_requested_at").IsNotNull())
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) CompleteCleanup(ctx context.Context, id JobID) error {
	q := r.db.Update("jobs").
		Set(goqu.Record{"cleanup_complete": true, "cleanup_error": nil, "cleanup_after": nil}).
		Where(goqu.C("id").Eq(id), goqu.C("delete_requested_at").IsNotNull())
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) RecordCleanupError(ctx context.Context, id JobID, cleanupErr error) error {
	retryAt := time.Now().UTC().Add(time.Minute).Format("2006-01-02T15:04:05.000Z")
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"cleanup_error":    cleanupErr.Error(),
			"cleanup_after":    retryAt,
			"cleanup_attempts": goqu.L("cleanup_attempts + 1"),
			"updated_at":       now(),
		}).
		Where(goqu.C("id").Eq(id))
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) ClearCleanupError(ctx context.Context, id JobID) error {
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"cleanup_error": nil,
			"cleanup_after": nil,
		}).
		Where(goqu.C("id").Eq(id))
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) RecoverJobs(ctx context.Context) error {
	message := "Interrupted by a restart"
	q := r.db.Update("jobs").
		Set(goqu.Record{
			"status":      JobFailed,
			"error":       message,
			"finished_at": now(),
			"updated_at":  now(),
		}).
		Where(goqu.C("status").Eq(JobRunning))
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) AddLog(ctx context.Context, id JobID, message string) error {
	q := r.db.Insert("job_logs").
		Rows(goqu.Record{
			"job_id":  id,
			"message": message,
		})
	_, err := q.Executor().ExecContext(ctx)
	return err
}

func (r *Repository) Logs(ctx context.Context, id JobID) ([]string, error) {
	q := r.db.From("job_logs").
		Select("message").
		Where(goqu.C("job_id").Eq(id)).
		Order(goqu.C("id").Asc())
	var lines []string
	if err := q.ScanValsContext(ctx, &lines); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if lines == nil {
		lines = []string{}
	}
	return lines, nil
}
