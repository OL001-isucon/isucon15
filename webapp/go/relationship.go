package isureal

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type FriendRequest struct {
	ID          string    `db:"id"`
	RequesterID string    `db:"requester_id"`
	RecipientID string    `db:"recipient_id"`
	Status      string    `db:"status"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

func getFriendRequest(ctx context.Context, db sqlx.QueryerContext, id string) (FriendRequest, error) {
	var request FriendRequest
	err := sqlx.GetContext(ctx, db, &request, `SELECT id, requester_id, recipient_id, status, created_at, updated_at FROM friend_requests WHERE id=$1`, id)
	return request, err
}

func beginRelationshipTx(ctx context.Context, db *sqlx.DB, actor, target string) (*sqlx.Tx, error) {
	if actor == target || target == "" {
		return nil, NewAPIError(http.StatusBadRequest, "invalid_relationship")
	}
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := lockRelationshipUsers(ctx, tx, actor, target); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// 2 人の関係 (申請、友達、フォロー、ブロック) の変更をペア単位で直列にする。
// users 行をロックすると、その人の投稿やログインまで待たせるので advisory lock を使う
func lockRelationshipUsers(ctx context.Context, tx *sqlx.Tx, actor, target string) error {
	if actor == target || target == "" {
		return NewAPIError(http.StatusBadRequest, "invalid_relationship")
	}
	var active int
	if err := tx.GetContext(ctx, &active, `
  SELECT (SELECT count(*) FROM users WHERE id IN ($1, $2) AND status = 'active')
  FROM pg_advisory_xact_lock(hashtext($3))
 `, actor, target, "rel:"+min(actor, target)+":"+max(actor, target)); err != nil {
		return err
	}
	if active != 2 {
		return NewAPIError(http.StatusNotFound, "relationship_not_found")
	}
	return nil
}

// lockUserSlot は利用者単位の排他 (投稿枠、セッション) を purpose ごとに分けて取る
func lockUserSlot(ctx context.Context, tx *sqlx.Tx, purpose, userID string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, purpose+":"+userID)
	return err
}

func isBlocked(ctx context.Context, db sqlx.QueryerContext, actor, target string) (bool, error) {
	var blocked bool
	err := sqlx.GetContext(ctx, db, &blocked, `SELECT EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`, actor, target)
	return blocked, err
}

func insertFriendship(ctx context.Context, tx *sqlx.Tx, request FriendRequest) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO friendships (id, user_low_id, user_high_id, source_request_id) VALUES ($1,$2,$3,$4)`, uuid.NewString(), min(request.RequesterID, request.RecipientID), max(request.RequesterID, request.RecipientID), request.ID)
	return err
}

func requireRelationshipChange(result sql.Result, err error, status int, code string) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return NewAPIError(status, code)
	}
	return nil
}
