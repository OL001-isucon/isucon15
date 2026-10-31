package isureal

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type createFriendRequestRequest struct {
	RecipientID string `json:"recipientId"`
}

type createFriendRequestResponse struct {
	RelationID string    `json:"relationId"`
	Status     string    `json:"status"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func createFriendRequestHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input createFriendRequestRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		tx, err := beginRelationshipTx(ctx, h.DB, actor, input.RecipientID)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// europe / america からは 1 文ごとに 100ms かかるので、判定に要る値を 1 往復で読み、書き込みも 1 往復にする
		low, high := min(actor, input.RecipientID), max(actor, input.RecipientID)
		var state struct {
			Blocked          bool    `db:"blocked"`
			Friends          bool    `db:"friends"`
			PendingID        *string `db:"pending_id"`
			PendingRequester *string `db:"pending_requester"`
		}
		if err := tx.GetContext(ctx, &state, `
   SELECT EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1)) AS blocked,
          EXISTS(SELECT 1 FROM friendships WHERE user_low_id=$3 AND user_high_id=$4) AS friends,
          (SELECT id FROM friend_requests WHERE status='pending' AND
            ((requester_id=$1 AND recipient_id=$2) OR (requester_id=$2 AND recipient_id=$1)) LIMIT 1) AS pending_id,
          (SELECT requester_id FROM friend_requests WHERE status='pending' AND
            ((requester_id=$1 AND recipient_id=$2) OR (requester_id=$2 AND recipient_id=$1)) LIMIT 1) AS pending_requester
  `, actor, input.RecipientID, low, high); err != nil {
			return err
		}
		if state.Blocked || state.Friends {
			return NewAPIError(http.StatusConflict, "relationship_conflict")
		}
		var request FriendRequest
		if state.PendingID != nil {
			if state.PendingRequester != nil && *state.PendingRequester == actor {
				return NewAPIError(http.StatusConflict, "relationship_conflict")
			}
			if err := tx.GetContext(ctx, &request, `
   WITH f AS (
     INSERT INTO friendships (id, user_low_id, user_high_id, source_request_id) VALUES ($2, $3, $4, $1)
   )
   UPDATE friend_requests SET status='accepted', updated_at=clock_timestamp() WHERE id=$1
   RETURNING id, requester_id, recipient_id, status, created_at, updated_at
  `, *state.PendingID, uuid.NewString(), low, high); err != nil {
				return err
			}
		} else {
			if err := tx.GetContext(ctx, &request, `
   INSERT INTO friend_requests (id, requester_id, recipient_id, status, created_at, updated_at)
   VALUES ($1,$2,$3,'pending',clock_timestamp(),clock_timestamp())
   RETURNING id, requester_id, recipient_id, status, created_at, updated_at
  `, uuid.NewString(), actor, input.RecipientID); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, createFriendRequestResponse{RelationID: request.ID, Status: request.Status, UpdatedAt: request.UpdatedAt})
	}
}

type transitionFriendRequestRequest struct {
	RequestID string `param:"requestId" json:"-"`
	Action    string `param:"action" json:"-"`
}

type transitionFriendRequestResponse createFriendRequestResponse

func transitionFriendRequestHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input transitionFriendRequestRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		var status string
		switch input.Action {
		case "accept":
			status = "accepted"
		case "reject":
			status = "rejected"
		case "cancel":
			status = "cancelled"
		default:
			return NewAPIError(http.StatusForbidden, "relationship_action_forbidden")
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		request, err := getFriendRequest(ctx, h.DB, input.RequestID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		}
		if err != nil {
			return err
		}
		if (status == "cancelled" && actor != request.RequesterID) || (status != "cancelled" && actor != request.RecipientID) {
			return NewAPIError(http.StatusForbidden, "relationship_action_forbidden")
		}
		tx, err := beginRelationshipTx(ctx, h.DB, request.RequesterID, request.RecipientID)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// 申請の行とブロックの有無を 1 往復で読み、友達の追加と状態の更新を 1 往復で書く (9 往復 → 6 往復)
		var state struct {
			FriendRequest
			Blocked bool `db:"blocked"`
		}
		if err := tx.GetContext(ctx, &state, `
   SELECT id, requester_id, recipient_id, status, created_at, updated_at,
          EXISTS(SELECT 1 FROM block_relations b
                 WHERE (b.blocker_id=r.requester_id AND b.blocked_id=r.recipient_id)
                    OR (b.blocker_id=r.recipient_id AND b.blocked_id=r.requester_id)) AS blocked
   FROM friend_requests r WHERE r.id=$1
  `, input.RequestID); err != nil {
			return err
		}
		if state.Status != "pending" {
			return NewAPIError(http.StatusForbidden, "relationship_action_forbidden")
		}
		if status == "accepted" && state.Blocked {
			return NewAPIError(http.StatusConflict, "relationship_conflict")
		}
		if err := tx.GetContext(ctx, &request, `
   WITH f AS (
     INSERT INTO friendships (id, user_low_id, user_high_id, source_request_id)
     SELECT $3, $4, $5, $1 WHERE $2::text = 'accepted'
   )
   UPDATE friend_requests SET status=$2, updated_at=clock_timestamp() WHERE id=$1
   RETURNING id, requester_id, recipient_id, status, created_at, updated_at
  `, state.ID, status, uuid.NewString(), min(state.RequesterID, state.RecipientID), max(state.RequesterID, state.RecipientID)); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, transitionFriendRequestResponse{RelationID: request.ID, Status: request.Status, UpdatedAt: request.UpdatedAt})
	}
}
