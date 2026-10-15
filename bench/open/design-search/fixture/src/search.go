package search

import "database/sql"

// Search returns the ids of the user's notes that match q, newest first.
func Search(db *sql.DB, userID int64, q string) ([]int64, error) {
	rows, err := db.Query(`
		SELECT id FROM notes
		WHERE user_id = $1
		  AND to_tsvector('simple', title || ' ' || body) @@ plainto_tsquery('simple', $2)
		ORDER BY updated_at DESC
		LIMIT 50`, userID, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
