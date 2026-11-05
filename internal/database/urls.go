package database

import (
	"database/sql"
	"errors"
	"github.com/lib/pq"
)

func InsertLink(db *sql.DB, longURL, shortCode string, userID *int64, isCustom bool) error {
	// Updated table name to match your actual "urls" table
	query := `
		INSERT INTO urls (short_code, long_url, user_id, is_custom)
		VALUES ($1, $2, $3, $4)
	`
	
	_, err := db.Exec(query, shortCode, longURL, userID, isCustom)
	if err != nil {
		// Check for PostgreSQL unique violation error code
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errors.New("alias is already taken")
		}
		return err
	}

	return nil
}
