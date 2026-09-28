package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Returns play counts per hour since the given time. With a timezone (IANA name), hours are
// bucketed in that zone and keyed by their wall-clock time (in the UTC location), which is
// exact even for zones with half-hour offsets. Without one, keys are the instants each hour
// starts, for the caller to place in the server's local time.
func GetHourlyPlayCounts(userId int, since time.Time, timezone string) (map[time.Time]int, error) {
	var rows pgx.Rows
	var err error
	if timezone != "" {
		rows, err = Pool.Query(context.Background(),
			`SELECT date_trunc('hour', timestamp AT TIME ZONE $3) AS hour, COUNT(*)
			FROM history
			WHERE user_id = $1 AND timestamp >= $2
			GROUP BY 1`,
			userId, since, timezone)
	} else {
		rows, err = Pool.Query(context.Background(),
			`SELECT date_trunc('hour', timestamp) AS hour, COUNT(*)
			FROM history
			WHERE user_id = $1 AND timestamp >= $2
			GROUP BY 1`,
			userId, since)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[time.Time]int)
	for rows.Next() {
		var hour time.Time
		var count int
		if err := rows.Scan(&hour, &count); err != nil {
			return nil, err
		}
		counts[hour] = count
	}
	return counts, rows.Err()
}
