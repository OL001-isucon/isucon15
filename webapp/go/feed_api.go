package isureal

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
)

type getFeedRequest struct {
	Feed           string     `param:"feed"`
	BeforeScore    *int64     `query:"beforeScore"`
	BeforePostedAt *time.Time `query:"beforePostedAt"`
	BeforeID       string     `query:"beforeId"`
}

type feedItemResponse struct {
	createPostResponse
	Score *int64 `json:"score,omitempty"`
}

type getFeedResponse struct {
	Items   []feedItemResponse `json:"items"`
	HasMore bool               `json:"hasMore"`
}

func getFeedHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getFeedRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		switch input.Feed {
		case "friends", "friends-of-friends", "public", "following":
		default:
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}
		if (input.BeforePostedAt == nil) != (input.BeforeID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ranked := input.Feed == "friends-of-friends"
		if ranked && (input.BeforeScore == nil) != (input.BeforeID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ctx := c.Request().Context()
		// 友達の友達の一覧は following と friends-of-friends の閲覧判定でしか使わない
		viewer, err := loadViewerContextOpts(ctx, h.DB, GetSession(c).UserID, input.Feed == "following" || ranked)
		if errors.Is(err, sql.ErrNoRows) {
			return c.JSON(http.StatusOK, getFeedResponse{Items: []feedItemResponse{}})
		}
		if err != nil {
			return err
		}
		// 今日投稿していなければ他人の投稿は 1 件も見えない
		if !viewer.HasPostedToday {
			return c.JSON(http.StatusOK, getFeedResponse{Items: []feedItemResponse{}})
		}
		following := map[string]bool{}
		if input.Feed == "following" {
			var followeeIDs []string
			if err := h.DB.SelectContext(ctx, &followeeIDs, `SELECT followee_id FROM follows WHERE follower_id = $1`, viewer.ID); err != nil {
				return err
			}
			for _, id := range followeeIDs {
				following[id] = true
			}
		}
		beforePostedAt, beforeID := input.BeforePostedAt, input.BeforeID
		if ranked {
			beforePostedAt, beforeID = nil, ""
		}
		// 開いている全投稿を取ってから Go で絞ると 1 回に数百行を運ぶので、著者と公開範囲は SQL で絞る。
		// friends と public は SQL の条件だけで閲覧可否が決まるので LIMIT も SQL に置く
		authors, limit := feedAuthors(viewer, following, input.Feed)
		if authors != nil && len(authors) == 0 {
			return c.JSON(http.StatusOK, getFeedResponse{Items: []feedItemResponse{}})
		}
		posts, err := selectPostsWithAuthor(ctx, h.DB, `
   AND p.author_id <> $1 AND w.ended_at IS NULL
   AND ($2::text[] IS NULL OR p.author_id = ANY($2))
   AND NOT (p.author_id = ANY($3))
   AND ($4::text IS NULL OR p.visibility = $4)
   AND ($5::timestamptz IS NULL OR (p.posted_at, p.id) < ($5, $6))
   ORDER BY p.posted_at DESC, p.id DESC
  `+limit, viewer.ID, authors, mapKeys(viewer.Blocked), feedVisibility(input.Feed), beforePostedAt, beforeID)
		if err != nil {
			return err
		}
		candidates := make([]PostWithAuthor, 0)
		for _, post := range posts {
			if viewer.canView(post.Post) && matchesFeed(viewer, following, post.Post, input.Feed) {
				candidates = append(candidates, post)
			}
		}
		var scores map[string]int64
		if ranked {
			if scores, err = recommendationScores(ctx, h.DB, viewer, candidates); err != nil {
				return err
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				return scores[candidates[i].ID] > scores[candidates[j].ID]
			})
		}
		items := make([]feedItemResponse, 0)
		hasMore := false
		for _, post := range candidates {
			score := scores[post.ID]
			if ranked && input.BeforeScore != nil && !rankedBefore(score, post.Post, *input.BeforeScore, *input.BeforePostedAt, input.BeforeID) {
				continue
			}
			// 51 件目は有無だけ分かればよいので、応答を作らない。署名の計算が重い
			if len(items) == 50 {
				hasMore = true
				break
			}
			item := feedItemResponse{createPostResponse: newPostResponse(post.Post, post.Author)}
			if ranked {
				item.Score = &score
			}
			items = append(items, item)
		}
		return c.JSON(http.StatusOK, getFeedResponse{Items: items, HasMore: hasMore})
	}
}

// feedAuthors はフィードごとに SQL で絞る著者の一覧を返す。nil は絞らないことを表す。
// 閲覧可否が著者だけで決まる friends と public では、51 件に絞る LIMIT も返す
func feedAuthors(viewer viewerContext, following map[string]bool, feed string) ([]string, string) {
	switch feed {
	case "friends":
		return mapKeys(viewer.Friends), "LIMIT 51"
	case "public":
		return nil, "LIMIT 51"
	case "following":
		return mapKeys(following), ""
	case "friends-of-friends":
		// 友達の友達は数千人になり、配列を SQL に渡すと行ごとの線形探索で 7 ms かかる (絞らなければ 1 ms)。
		// 順位付けに全候補が要るので LIMIT も置けず、SQL で絞る利点がないので Go 側の判定に任せる
		return nil, ""
	default:
		return []string{}, ""
	}
}

// feedVisibility は SQL で絞る公開範囲を返す。nil は絞らないことを表す
func feedVisibility(feed string) *string {
	if feed == "public" {
		visibility := "public"
		return &visibility
	}
	return nil
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func rankedBefore(score int64, post Post, beforeScore int64, beforePostedAt time.Time, beforeID string) bool {
	if score != beforeScore {
		return score < beforeScore
	}
	if !post.PostedAt.Equal(beforePostedAt) {
		return post.PostedAt.Before(beforePostedAt)
	}
	return post.ID < beforeID
}

func recommendationScores(ctx context.Context, db sqlx.QueryerContext, viewer viewerContext, posts []PostWithAuthor) (map[string]int64, error) {
	scores := make(map[string]int64, len(posts))
	if len(posts) == 0 {
		return scores, nil
	}
	authorIDs := make([]string, 0, len(posts))
	postIDs := make([]string, 0, len(posts))
	for _, post := range posts {
		authorIDs = append(authorIDs, post.AuthorID)
		postIDs = append(postIDs, post.ID)
	}

	query, args, err := sqlx.In(`SELECT user_low_id, user_high_id FROM friendships WHERE user_low_id IN (?) OR user_high_id IN (?)`, authorIDs, authorIDs)
	if err != nil {
		return nil, err
	}
	var friendships []struct {
		UserLowID  string `db:"user_low_id"`
		UserHighID string `db:"user_high_id"`
	}
	if err := sqlx.SelectContext(ctx, db, &friendships, sqlx.Rebind(sqlx.DOLLAR, query), args...); err != nil {
		return nil, err
	}
	connections := map[string]int64{}
	for _, friendship := range friendships {
		connections[friendship.UserLowID] += int64(viewer.MutualFriends[friendship.UserHighID])
		connections[friendship.UserHighID] += int64(viewer.MutualFriends[friendship.UserLowID])
	}

	query, args, err = sqlx.In(`
  SELECT r.post_id, r.reactor_id AS user_id FROM realmojis r
  JOIN users u ON u.id = r.reactor_id AND u.status = 'active'
  WHERE r.post_id IN (?) AND r.replaced_at IS NULL AND r.deleted_at IS NULL
  UNION
  SELECT c.post_id, c.author_id AS user_id FROM comments c
  JOIN users u ON u.id = c.author_id AND u.status = 'active'
  WHERE c.post_id IN (?) AND c.deleted_at IS NULL
 `, postIDs, postIDs)
	if err != nil {
		return nil, err
	}
	var engagements []struct {
		PostID string `db:"post_id"`
		UserID string `db:"user_id"`
	}
	if err := sqlx.SelectContext(ctx, db, &engagements, sqlx.Rebind(sqlx.DOLLAR, query), args...); err != nil {
		return nil, err
	}
	friendEngagements := map[string]int64{}
	for _, engagement := range engagements {
		if viewer.Friends[engagement.UserID] {
			friendEngagements[engagement.PostID]++
		}
	}

	for _, post := range posts {
		scores[post.ID] = 10*friendEngagements[post.ID] + 3*int64(viewer.MutualFriends[post.AuthorID]) + min(connections[post.AuthorID], 30)
	}
	return scores, nil
}

func matchesFeed(viewer viewerContext, following map[string]bool, post Post, feed string) bool {
	switch feed {
	case "public":
		return post.Visibility == "public"
	case "following":
		return following[post.AuthorID]
	case "friends":
		return viewer.Friends[post.AuthorID]
	case "friends-of-friends":
		return !viewer.Friends[post.AuthorID] && viewer.FriendsOfFriends[post.AuthorID]
	default:
		return false
	}
}
