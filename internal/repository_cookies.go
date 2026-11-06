package internal

import (
	"context"

	"github.com/doug-martin/goqu/v9"
)

type cookieRow struct {
	Domain  string `db:"domain"`
	Content string `db:"content"`
}

func (r *Repository) PutCookies(ctx context.Context, cookies Cookies) error {
	return r.db.WithTx(func(tx *goqu.TxDatabase) error {
		for domain, content := range cookies {
			q := tx.Insert("cookies").
				Rows(goqu.Record{
					"domain":     domain,
					"content":    content,
					"updated_at": now(),
				}).
				OnConflict(goqu.DoUpdate("domain", goqu.Record{
					"content":    content,
					"updated_at": now(),
				}))
			if _, err := q.Executor().ExecContext(ctx); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}

func (r *Repository) Cookies(ctx context.Context) (Cookies, error) {
	q := r.db.From("cookies").
		Select(&cookieRow{}).
		Order(goqu.C("domain").Asc())
	var rows []cookieRow
	if err := q.ScanStructsContext(ctx, &rows); err != nil {
		return nil, err
	}
	cookies := make(Cookies, len(rows))
	for _, row := range rows {
		cookies[row.Domain] = row.Content
	}
	return cookies, nil
}

func (r *Repository) DeleteCookies(ctx context.Context, domain string) error {
	q := r.db.Delete("cookies")
	if domain != "" {
		q = q.Where(goqu.C("domain").Eq(domain))
	}
	_, err := q.Executor().ExecContext(ctx)
	return err
}
