package isureal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type getNotificationsResponse struct {
	ID              string    `json:"id" db:"id"`
	PostingWindowID string    `json:"postingWindowId" db:"posting_window_id"`
	Region          string    `json:"region" db:"region"`
	CreatedAt       time.Time `json:"createdAt" db:"created_at"`
}

// 起こす知らせが届かなかったときの確認の間隔
const notificationRecheckInterval = 60 * time.Second

func getNotificationsHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		session := GetSession(c)
		userID := session.UserID
		var connectedAt time.Time
		if err := h.DB.GetContext(ctx, &connectedAt, `SELECT clock_timestamp()`); err != nil {
			return err
		}
		response := c.Response()
		response.Header().Set("Content-Type", "text/event-stream")
		response.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(response)
		if err := controller.Flush(); err != nil {
			return nil
		}
		ticker := time.NewTicker(notificationRecheckInterval)
		defer ticker.Stop()
		for {
			// DB を確認する前に取得する。確認の直後に Wake されても取りこぼさない
			changed := h.Notifications.Changed()
			user, err := getUser(ctx, h.DB, userID)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
					c.Logger().Error("failed to get notification recipient", "error", err)
				}
				return nil
			}
			if user.Region != session.Region {
				return nil
			}
			// 通知の中身は「期間 ID、リージョン、期間の開始時刻」と利用者ごとの通知 ID だけなので、
			// 通知行を持たず、接続後に始まってまだ開いている期間から組み立てる。通知 ID は期間と利用者から決定的に作る
			var notification getNotificationsResponse
			err = h.DB.GetContext(ctx, &notification, `
    SELECT w.id AS posting_window_id, w.region, w.starts_at AS created_at
    FROM posting_windows w
    WHERE w.region = $1 AND w.starts_at > $2 AND w.ended_at IS NULL
    ORDER BY w.starts_at LIMIT 1
   `, session.Region, connectedAt)
			if err == nil {
				notification.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(notification.PostingWindowID+":"+userID)).String()
				data, err := json.Marshal(notification)
				if err != nil {
					c.Logger().Error("failed to encode notification", "error", err)
					return nil
				}
				if _, err := fmt.Fprintf(response, "data: %s\n\n", data); err != nil {
					return nil
				}
				_ = controller.Flush()
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				if ctx.Err() == nil {
					c.Logger().Error("failed to get notification", "error", err)
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-changed:
			case <-ticker.C:
			}
		}
	}
}
