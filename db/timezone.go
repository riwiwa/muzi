package db

import "context"

// The user's IANA timezone, or "" to use the server's local time
func GetUserTimezone(userId int) (string, error) {
	var tz *string
	err := Pool.QueryRow(context.Background(), "SELECT timezone FROM users WHERE pk = $1", userId).Scan(&tz)
	if err != nil || tz == nil {
		return "", err
	}
	return *tz, nil
}

// Sets the user's timezone; "" goes back to the server's local time
func SetUserTimezone(userId int, tz string) error {
	_, err := Pool.Exec(context.Background(), "UPDATE users SET timezone = NULLIF($1, '') WHERE pk = $2", tz, userId)
	return err
}
