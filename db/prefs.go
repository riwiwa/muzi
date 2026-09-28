package db

import "context"

// The user's saved profile chart choices, keyed by URL parameter name (see web/prefs.go)
func GetProfilePrefs(userId int) (map[string]string, error) {
	prefs := map[string]string{}
	err := Pool.QueryRow(context.Background(),
		"SELECT profile_prefs FROM users WHERE pk = $1", userId).Scan(&prefs)
	return prefs, err
}

func SetProfilePrefs(userId int, prefs map[string]string) error {
	_, err := Pool.Exec(context.Background(),
		"UPDATE users SET profile_prefs = $1 WHERE pk = $2", prefs, userId)
	return err
}
