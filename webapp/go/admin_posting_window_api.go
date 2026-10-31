package isureal

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type openPostingWindowRequest struct {
	Region        string `json:"region"`
	LocalDate     string `json:"localDate"`
	OnTimeSeconds *int64 `json:"onTimeSeconds"`
}

type openPostingWindowResponse struct {
	ID             string    `json:"id"`
	Region         string    `json:"region"`
	LocalDate      string    `json:"localDate"`
	StartsAt       time.Time `json:"startsAt"`
	OnTimeDeadline time.Time `json:"onTimeDeadline"`
}

func openPostingWindowHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input openPostingWindowRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		region := input.Region
		localDate, err := time.Parse(time.DateOnly, input.LocalDate)
		if !validRegion(region) || err != nil {
			return NewAPIError(http.StatusBadRequest, "invalid_posting_window")
		}
		seconds := int64(24 * 60 * 60)
		if input.OnTimeSeconds != nil {
			seconds = *input.OnTimeSeconds
		}
		ctx := c.Request().Context()
		tx, err := h.DB.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// 同じリージョンの開始だけを直列にする。users 全体をロックすると他の書き込みを止める
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, "posting-window:"+region); err != nil {
			return err
		}
		var conflict bool
		if err := tx.GetContext(ctx, &conflict, `SELECT EXISTS(SELECT 1 FROM posting_windows WHERE region = $1 AND local_date >= $2)`, region, localDate); err != nil {
			return err
		}
		if conflict {
			return NewAPIError(http.StatusConflict, "posting_window_conflict")
		}
		var startsAt time.Time
		if err := tx.GetContext(ctx, &startsAt, `SELECT clock_timestamp()`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE posting_windows SET ended_at = $2 WHERE region = $1 AND ended_at IS NULL`, region, startsAt); err != nil {
			return err
		}
		windowID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
   INSERT INTO posting_windows (id, region, local_date, starts_at, on_time_deadline)
   VALUES ($1, $2, $3, $4, $5)
  `, windowID, region, localDate, startsAt, startsAt.Add(time.Duration(seconds)*time.Second)); err != nil {
			return err
		}
		window, err := getPostingWindow(ctx, tx, windowID)
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		// 通知の INSERT (数万行、0.4〜0.8 秒) を切り替えのトランザクションに含めると、その間ずっと旧期間の行を握り、
		// FOR SHARE で待つ投稿作成を全部止めてしまう。切り替えを先に確定してから通知を入れる
		if _, err := h.DB.ExecContext(ctx, `
   INSERT INTO notifications (id, user_id, posting_window_id, created_at)
   SELECT gen_random_uuid()::text, id, $2, $3 FROM users WHERE region = $1 AND status = 'active'
  `, region, windowID, startsAt); err != nil {
			return err
		}
		h.wakeNotifications(region)
		return c.JSON(http.StatusCreated, openPostingWindowResponse{
			ID: window.ID, Region: window.Region, LocalDate: window.LocalDate.Format(time.DateOnly),
			StartsAt: window.StartsAt, OnTimeDeadline: window.OnTimeDeadline,
		})
	}
}
