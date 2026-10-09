package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// shareOwner selects one of the two fixed cost-share relations. It never comes
// from a request; statements below interpolate only these internal constants.
type shareOwner string

// The two kinds of content whose costs name individual members.
const (
	itemShares shareOwner = "item"
	stopShares shareOwner = "stop"
)

// readShares reads the selected costs' shares in their stored order, by owner.
// where and its arguments are supplied by repository statements, not clients.
func readShares(ctx context.Context, q querier, owner shareOwner, where string, args ...any) (map[uuid.UUID][]domain.CostShare, error) {
	rows, err := q.Query(ctx, `SELECT s.`+string(owner)+`_id, s.user_id, (s.amount * 100)::bigint
	 FROM `+string(owner)+`_cost_shares s WHERE `+where+` ORDER BY s.`+string(owner)+`_id, s.position`, args...)
	if err != nil {
		return nil, fmt.Errorf("query cost shares: %w", err)
	}
	defer rows.Close()
	shares := make(map[uuid.UUID][]domain.CostShare)
	for rows.Next() {
		var id uuid.UUID
		var share domain.CostShare
		if err := rows.Scan(&id, &share.UserID, &share.Amount); err != nil {
			return nil, fmt.Errorf("read cost share: %w", err)
		}
		shares[id] = append(shares[id], share)
	}
	return shares, rows.Err()
}

// writeShares atomically replaces one place's or stop's individual cost shares
// within the transaction that writes its other cost fields.
func writeShares(ctx context.Context, tx pgx.Tx, owner shareOwner, id uuid.UUID, shares []domain.CostShare) error {
	column, table := string(owner)+"_id", string(owner)+"_cost_shares"
	if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE `+column+` = $1`, id); err != nil {
		return fmt.Errorf("clear cost shares: %w", err)
	}
	for position, share := range shares {
		if _, err := tx.Exec(ctx, `INSERT INTO `+table+` (`+column+`, user_id, position, amount) VALUES ($1, $2, $3, $4::numeric)`,
			id, share.UserID, position, moneyParam(share.Amount)); err != nil {
			return fmt.Errorf("add cost share: %w", err)
		}
	}
	return nil
}
