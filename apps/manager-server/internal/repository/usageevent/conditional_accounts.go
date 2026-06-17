package usageevent

import (
	"context"
	"database/sql"
)

type ConditionalAccountStat struct {
	FileName             string
	AccountSnapshot      string
	AuthLabelSnapshot    string
	AuthProviderSnapshot string
	AuthIndex            string
	SourceHash           string
	Calls                int64
	SuccessCalls         int64
	FailureCalls         int64
	UnauthorizedCalls    int64
	LastSeenMS           int64
}

func (r *repository) ConditionalAccountsBetween(ctx context.Context, fromMS, toMS int64) ([]ConditionalAccountStat, error) {
	if fromMS <= 0 || toMS <= fromMS {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `select
	coalesce(auth_file_snapshot, ''),
	coalesce(account_snapshot, ''),
	coalesce(auth_label_snapshot, ''),
	coalesce(nullif(auth_provider_snapshot, ''), provider, ''),
	coalesce(auth_index, ''),
	coalesce(source_hash, ''),
	count(*),
	sum(case when failed = 0 then 1 else 0 end),
	sum(case when failed = 1 then 1 else 0 end),
	sum(case when fail_status_code = 401 then 1 else 0 end),
	max(timestamp_ms)
from usage_events
where timestamp_ms >= ? and timestamp_ms < ?
group by auth_file_snapshot, account_snapshot, auth_label_snapshot,
	coalesce(nullif(auth_provider_snapshot, ''), provider, ''), auth_index, source_hash
order by max(timestamp_ms) desc`, fromMS, toMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ConditionalAccountStat, 0)
	for rows.Next() {
		var item ConditionalAccountStat
		var unauthorized sql.NullInt64
		if err := rows.Scan(
			&item.FileName,
			&item.AccountSnapshot,
			&item.AuthLabelSnapshot,
			&item.AuthProviderSnapshot,
			&item.AuthIndex,
			&item.SourceHash,
			&item.Calls,
			&item.SuccessCalls,
			&item.FailureCalls,
			&unauthorized,
			&item.LastSeenMS,
		); err != nil {
			return nil, err
		}
		item.UnauthorizedCalls = unauthorized.Int64
		items = append(items, item)
	}
	return items, rows.Err()
}
