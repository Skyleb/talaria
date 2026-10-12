package store

import "context"

// VisibleUserIDs returns users who can "see" userID: accepted friends plus
// members of shared groups (used for presence fan-out).
func (s *Store) VisibleUserIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT uid FROM (
			SELECT CASE WHEN requester_id = ? THEN addressee_id ELSE requester_id END AS uid
			FROM friendships
			WHERE (requester_id = ? OR addressee_id = ?) AND status = 'accepted'
			UNION
			SELECT m2.user_id FROM group_members m1
			JOIN group_members m2 ON m2.group_id = m1.group_id
			WHERE m1.user_id = ?
			UNION
			SELECT p2.user_id FROM dm_participants p1
			JOIN dm_participants p2 ON p2.channel_id = p1.channel_id
			WHERE p1.user_id = ?
		) WHERE uid != ?`,
		userID, userID, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
