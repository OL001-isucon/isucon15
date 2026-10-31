package isureal

import (
	"bytes"
	"database/sql"
	"errors"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type replaceRealMojiRequest struct {
	PostID       string                `param:"postId" json:"-"`
	Image        *multipart.FileHeader `form:"image"`
	ReactionType string                `form:"reactionType"`
}

type replaceRealMojiResponse struct {
	ID           string                   `json:"id"`
	Reactor      relationshipUserResponse `json:"reactor"`
	ReactionType string                   `json:"reactionType"`
	ImageURL     string                   `json:"imageUrl"`
	CreatedAt    time.Time                `json:"createdAt"`
}

func newRealMojiResponse(reaction RealMoji, user User) replaceRealMojiResponse {
	return replaceRealMojiResponse{ID: reaction.ID, Reactor: newRelationshipUserResponse(user), ReactionType: reaction.ReactionType, ImageURL: signedImageURL(user, reaction.ImageID), CreatedAt: reaction.CreatedAt}
}

func replaceRealMojiHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input replaceRealMojiRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if input.Image == nil {
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}
		switch input.ReactionType {
		case "like", "smile", "surprise", "heart", "laugh", "instant":
		default:
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}
		file, err := input.Image.Open()
		if err != nil {
			return err
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}
		if img.Bounds().Dx() > 512 || img.Bounds().Dy() > 512 {
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}

		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		post, err := getPost(ctx, h.DB, input.PostID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		if err != nil {
			return err
		}
		if actor == post.AuthorID {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		// 読み取りは users の行ロックを取る前に済ませる。以前はロック後に閲覧可否 (6 往復) と
		// 自分の取得を行っていて、europe / america からだと著者の行を 1 秒以上握っていた。
		// 著者の行は人気の投稿ほど他の RealMoji や友達申請と競合する
		user, err := getUser(ctx, h.DB, actor)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		}
		if err != nil {
			return err
		}
		allowed, err := canViewPost(ctx, h.DB, actor, post.ID)
		if err != nil {
			return err
		}
		if !allowed {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		imageID := uuid.NewString()
		if err := h.Images.SaveNew(imageID, data); err != nil {
			return err
		}
		tx, err := h.DB.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// 画像の行は users 行と競合しないので、ロックの前に入れておく (失敗時は tx ごと戻る)
		if _, err := tx.ExecContext(ctx, `INSERT INTO image_objects(id,owner_user_id,origin_region,media_type) VALUES($1,$2,$3,'image/jpeg')`, imageID, actor, h.Config.Region); err != nil {
			return err
		}
		// 投稿者や自分の users 行ではなく投稿の行を共有ロックし、同じ投稿への自分の送信だけを advisory lock で直列にする。
		// 投稿者の行を握ると、その人への友達申請やコメントまで待たせてしまう
		if err := lockPostShared(ctx, tx, post.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "realmoji:"+post.ID+":"+actor); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE realmojis SET replaced_at=clock_timestamp() WHERE post_id=$1 AND reactor_id=$2 AND replaced_at IS NULL AND deleted_at IS NULL`, post.ID, actor); err != nil {
			return err
		}
		var reaction RealMoji
		if err := tx.GetContext(ctx, &reaction, `
   INSERT INTO realmojis(id,post_id,reactor_id,image_id,reaction_type,created_at) VALUES($1,$2,$3,$4,$5,clock_timestamp())
   RETURNING id, post_id, reactor_id, image_id, reaction_type, created_at, replaced_at, deleted_at
  `, uuid.NewString(), post.ID, actor, imageID, input.ReactionType); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		h.Images.Distribute(imageID, data)
		return c.JSON(http.StatusCreated, newRealMojiResponse(reaction, user))
	}
}

type getRealMojisRequest struct {
	PostID          string     `param:"postId"`
	BeforeCreatedAt *time.Time `query:"beforeCreatedAt"`
	BeforeID        string     `query:"beforeId"`
}

type getRealMojisResponse struct {
	Items   []replaceRealMojiResponse `json:"items"`
	HasMore bool                      `json:"hasMore"`
}

func getRealMojisHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getRealMojisRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if (input.BeforeCreatedAt == nil) != (input.BeforeID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ctx := c.Request().Context()
		allowed, err := canViewPost(ctx, h.DB, GetSession(c).UserID, input.PostID)
		if err != nil {
			return err
		}
		if !allowed {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		reactions, err := selectCurrentRealMojis(ctx, h.DB, `AND r.post_id=$1 AND ($2::timestamptz IS NULL OR (r.created_at,r.id)<($2,$3)) ORDER BY r.created_at DESC,r.id DESC LIMIT 51`, input.PostID, input.BeforeCreatedAt, input.BeforeID)
		if err != nil {
			return err
		}
		hasMore := len(reactions) > 50
		if hasMore {
			reactions = reactions[:50]
		}
		items := make([]replaceRealMojiResponse, 0, len(reactions))
		for _, reaction := range reactions {
			items = append(items, newRealMojiResponse(reaction.RealMoji, reaction.Reactor))
		}
		return c.JSON(http.StatusOK, getRealMojisResponse{Items: items, HasMore: hasMore})
	}
}

type deleteRealMojiRequest struct {
	RealMojiID string `param:"realMojiId" json:"-"`
}

func deleteRealMojiHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input deleteRealMojiRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		tx, err := h.DB.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// RealMoji 行の FOR UPDATE で足りるので、users 行はロックしない
		var record struct {
			ReactorID string `db:"reactor_id"`
			AuthorID  string `db:"author_id"`
		}
		err = tx.GetContext(ctx, &record, `SELECT r.reactor_id,p.author_id FROM realmojis r JOIN posts p ON p.id=r.post_id WHERE r.id=$1 AND r.deleted_at IS NULL FOR UPDATE OF r`, input.RealMojiID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "interaction_not_found")
		}
		if err != nil {
			return err
		}
		if actor != record.ReactorID && actor != record.AuthorID {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE realmojis SET deleted_at=clock_timestamp() WHERE id=$1`, input.RealMojiID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}
