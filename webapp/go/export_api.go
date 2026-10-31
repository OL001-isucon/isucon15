package isureal

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

type exportPostResponse struct {
	createPostResponse
	ImagePath string `json:"imagePath"`
}

func exportPostsHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		author, err := getUser(ctx, h.DB, userID)
		if err != nil {
			return err
		}
		var posts []struct {
			Post
			OnDisk         bool      `db:"on_disk"`
			OriginRegion   *string   `db:"origin_region"`
			ImageCreatedAt time.Time `db:"image_created_at"`
		}
		if err := h.DB.SelectContext(ctx, &posts, `
   SELECT `+postColumns+`, i.on_disk, i.origin_region, i.created_at AS image_created_at
   FROM posts p
   JOIN posting_windows w ON w.id = p.posting_window_id
   JOIN image_meta i ON i.id = p.image_id
   WHERE p.author_id = $1 AND p.deleted_at IS NULL
   ORDER BY p.posted_at, p.id
  `, userID); err != nil {
			return err
		}
		initialRefs := make([]initialImageRef, 0, len(posts))
		for _, post := range posts {
			if !post.OnDisk {
				initialRefs = append(initialRefs, initialImageRef{ID: post.ImageID, CreatedAt: post.ImageCreatedAt})
			}
		}
		initialImages, err := loadInitialImages(ctx, h, initialRefs)
		if err != nil {
			return err
		}

		var body bytes.Buffer
		writer := zip.NewWriter(&body)
		defer writer.Close()
		items := make([]exportPostResponse, 0, len(posts))
		for _, post := range posts {
			imageData := initialImages[post.ImageID]
			if post.OnDisk {
				imageData, err = h.Images.LoadNew(ctx, post.ImageID, post.OriginRegion)
				if err != nil {
					return err
				}
			}
			imagePath := fmt.Sprintf("images/%06d.jpg", len(items)+1)
			image, err := writer.CreateHeader(&zip.FileHeader{Name: imagePath, Method: zip.Store})
			if err != nil {
				return err
			}
			if _, err := image.Write(imageData); err != nil {
				return err
			}
			items = append(items, exportPostResponse{createPostResponse: newPostResponse(post.Post, author), ImagePath: imagePath})
		}
		manifest, err := writer.Create("posts.json")
		if err != nil {
			return err
		}
		if err := json.NewEncoder(manifest).Encode(items); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="isureal-posts.zip"`)
		c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
		return c.Blob(http.StatusOK, "application/zip", body.Bytes())
	}
}
