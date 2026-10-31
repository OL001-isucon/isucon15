package isureal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
)

type getImageRequest struct {
	ImageID string `param:"imageId" json:"-"`
	Exp     string `query:"exp" json:"-"`
	Sig     string `query:"sig" json:"-"`
}

func getImageHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		var input getImageRequest
		if err := c.Bind(&input); err != nil {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		query, err := url.ParseQuery(c.Request().URL.RawQuery)
		if err != nil || len(query["exp"]) != 1 || len(query["sig"]) != 1 {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		exp, err := strconv.ParseInt(input.Exp, 10, 64)
		if err != nil || strconv.FormatInt(exp, 10) != input.Exp || exp <= time.Now().Unix() {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		sig, err := base64.RawURLEncoding.Strict().DecodeString(input.Sig)
		if err != nil || len(sig) != sha256.Size || base64.RawURLEncoding.EncodeToString(sig) != input.Sig {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}

		// 削除状態を毎回確認する。即時反映も5秒以内の許容範囲に含まれる。
		// リージョン間は 1 往復 100ms なので、鍵と削除状態を 1 回で取得する。本体は読まない
		ctx := c.Request().Context()
		var object struct {
			SecretKeyHash string    `db:"secret_key_hash"`
			MediaType     string    `db:"media_type"`
			OriginRegion  *string   `db:"origin_region"`
			OnDisk        bool      `db:"on_disk"`
			CreatedAt     time.Time `db:"created_at"`
			Available     bool      `db:"available"`
		}
		err = h.DB.GetContext(ctx, &object, `
   SELECT u.secret_key_hash, i.media_type, i.origin_region, i.on_disk, i.created_at,
     COALESCE(u.profile_image_id = i.id, false)
     OR EXISTS (
       SELECT 1 FROM posts p
       WHERE p.image_id = i.id AND p.author_id = u.id AND p.deleted_at IS NULL
     )
     OR EXISTS (
       SELECT 1 FROM realmojis r
       JOIN posts p ON p.id = r.post_id AND p.deleted_at IS NULL
       JOIN users author ON author.id = p.author_id AND author.status = 'active'
       WHERE r.image_id = i.id AND r.reactor_id = u.id AND r.replaced_at IS NULL AND r.deleted_at IS NULL
     ) AS available
   FROM image_meta i JOIN users u ON u.id = i.owner_user_id
   WHERE i.id = $1 AND u.status = 'active'
  `, input.ImageID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		if err != nil {
			return err
		}
		key, err := imageSigningKey(object.SecretKeyHash)
		if err != nil {
			return err
		}
		if !verifyImageSignature(key, input.ImageID, exp, sig) {
			c.Set("abuse", "invalid_signature")
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		if !object.Available || exp <= time.Now().Unix() {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}

		var data []byte
		if object.OnDisk {
			data, err = h.Images.LoadNew(ctx, input.ImageID, object.OriginRegion)
		} else {
			data, err = loadInitialImage(ctx, h, input.ImageID, object.CreatedAt)
		}
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, object.MediaType, data)
	}
}

// 初期画像は中身が変わらないので、一度読んだら手元の控えから返す
// created_at を渡してパーティションを 1 つに絞る
func loadInitialImage(ctx context.Context, h *HandlerContext, imageID string, createdAt time.Time) ([]byte, error) {
	if data, ok := h.Images.LoadCached(imageID); ok {
		return data, nil
	}
	var data []byte
	if err := h.DB.GetContext(ctx, &data, `SELECT data FROM image_objects WHERE id = $1 AND created_at = $2`, imageID, createdAt); err != nil {
		return nil, err
	}
	h.Images.SaveCached(imageID, data)
	return data, nil
}

type initialImageRef struct {
	ID        string
	CreatedAt time.Time
}

// loadInitialImages は控えに無い分だけをまとめて DB から読む。created_at も渡してパーティションを絞る
func loadInitialImages(ctx context.Context, h *HandlerContext, refs []initialImageRef) (map[string][]byte, error) {
	images := make(map[string][]byte, len(refs))
	missingIDs := make([]string, 0)
	missingCreatedAt := make([]time.Time, 0)
	for _, ref := range refs {
		if data, ok := h.Images.LoadCached(ref.ID); ok {
			images[ref.ID] = data
		} else {
			missingIDs = append(missingIDs, ref.ID)
			missingCreatedAt = append(missingCreatedAt, ref.CreatedAt)
		}
	}
	if len(missingIDs) == 0 {
		return images, nil
	}
	var rows []struct {
		ID   string `db:"id"`
		Data []byte `db:"data"`
	}
	if err := h.DB.SelectContext(ctx, &rows, `SELECT id, data FROM image_objects WHERE id = ANY($1) AND created_at = ANY($2)`, missingIDs, missingCreatedAt); err != nil {
		return nil, err
	}
	for _, row := range rows {
		images[row.ID] = row.Data
		h.Images.SaveCached(row.ID, row.Data)
	}
	return images, nil
}
