package db

import (
	"context"
	"time"
)

// Returns play counts per hour since the given time, keyed by the start of each hour.
// Callers bin these into local days/hours so they line up with the rest of the UI.
func GetHourlyPlayCounts(userId int, since time.Time) (map[time.Time]int, error) {
	rows, err := Pool.Query(context.Background(),
		`SELECT date_trunc('hour', timestamp) AS hour, COUNT(*)
		FROM history
		WHERE user_id = $1 AND timestamp >= $2
		GROUP BY 1`,
		userId, since)
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
