package analytics

import (
	"context"
	"fmt"
	"strconv"
)

// Prune deletes analytics rows older than the retention period (400 days).
func (r *Recorder) Prune(ctx context.Context) error {
	days := strconv.Itoa(retentionDays)
	for _, q := range []string{
		`DELETE FROM page_view_stats WHERE bucket < now() - interval '` + days + ` days'`,
		`DELETE FROM page_visitors WHERE day < current_date - ` + days,
		`DELETE FROM page_visitor_hours WHERE hour < now() - interval '` + days + ` days'`,
	} {
		if _, err := r.db.Exec(ctx, q); err != nil {
			return fmt.Errorf("analytics prune: %w", err)
		}
	}
	return nil
}
