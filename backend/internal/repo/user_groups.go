package repo

import "context"

// AuthorizedGroup is the safe group summary shown to the current user.
// Node membership and node configuration are intentionally not included.
type AuthorizedGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ListAuthorizedGroups returns only groups explicitly assigned to userID.
func (r *GroupRepo) ListAuthorizedGroups(ctx context.Context, userID int64) ([]AuthorizedGroup, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ng.id, ng.name
		FROM user_node_groups ung
		JOIN node_groups ng ON ng.id=ung.group_id
		WHERE ung.user_id=$1
		ORDER BY ng.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]AuthorizedGroup, 0)
	for rows.Next() {
		var group AuthorizedGroup
		if err := rows.Scan(&group.ID, &group.Name); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}
