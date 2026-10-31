package isureal

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type searchUsersRequest struct {
	Q              string `query:"q"`
	BeforeUsername string `query:"beforeUsername"`
	BeforeID       string `query:"beforeId"`
}

type searchUsersResponse struct {
	Items   []relationshipUserResponse `json:"items"`
	HasMore bool                       `json:"hasMore"`
}

func normalizeSearchText(value string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(value)))
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(value string) string {
	return likeEscaper.Replace(value)
}

// 名前は多言語で、lower では ß などの折りたたみが再現できないので、Go で正規化した値を DB に持つ
func userSearchText(username, displayName string) string {
	return normalizeSearchText(username) + "\n" + normalizeSearchText(displayName)
}

func refreshUserSearchText(ctx context.Context, db sqlx.ExtContext, userID string) error {
	var user struct {
		Username    string `db:"username"`
		DisplayName string `db:"display_name"`
	}
	if err := sqlx.GetContext(ctx, db, &user, `SELECT username, display_name FROM users WHERE id = $1`, userID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE users SET search_text = $2 WHERE id = $1`, userID, userSearchText(user.Username, user.DisplayName))
	return err
}

// BackfillUserSearchText は初期ユーザーと、reset.sql が名前を戻したユーザーの値を作る
func BackfillUserSearchText(ctx context.Context, db *sqlx.DB) error {
	var users []struct {
		ID          string `db:"id"`
		Username    string `db:"username"`
		DisplayName string `db:"display_name"`
	}
	if err := db.SelectContext(ctx, &users, `SELECT id, username, display_name FROM users WHERE search_text IS NULL`); err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}
	ids := make([]string, 0, len(users))
	texts := make([]string, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
		texts = append(texts, userSearchText(user.Username, user.DisplayName))
	}
	_, err := db.ExecContext(ctx, `
   UPDATE users u SET search_text = d.search_text
   FROM unnest($1::text[], $2::text[]) AS d(id, search_text)
   WHERE u.id = d.id
  `, ids, texts)
	return err
}

func searchUsersHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input searchUsersRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if !validUserText(input.Q, 1, 100) {
			return NewAPIError(http.StatusBadRequest, "invalid_relationship")
		}
		if (input.BeforeUsername == "") != (input.BeforeID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ctx := c.Request().Context()
		viewer := GetSession(c).UserID
		query := normalizeSearchText(input.Q)
		// NUL は DB に渡せず、名前にも含まれない
		if strings.ContainsRune(query, 0) {
			return c.JSON(http.StatusOK, searchUsersResponse{Items: []relationshipUserResponse{}})
		}
		blocked, err := getBlockedUserIDs(ctx, h.DB, viewer)
		if err != nil {
			return err
		}
		// 全ユーザーを読まないよう DB で絞る。比較は下の Go の処理が正で、search_text は同じ正規化の値を持つ
		// search_text が NULL の行は無条件に通すので、その数だけ LIMIT を増やす
		var users []User
		if err := h.DB.SelectContext(ctx, &users, `SELECT `+strings.Join(userColumns, ", ")+` FROM users WHERE status='active' AND id<>$1 AND ($2='' OR (username,id)>($2,$3)) AND (search_text IS NULL OR search_text LIKE $4) ORDER BY username, id LIMIT $5::bigint + (SELECT count(*) FROM users WHERE search_text IS NULL)`, viewer, input.BeforeUsername, input.BeforeID, "%"+escapeLike(query)+"%", 51+len(blocked)); err != nil {
			return err
		}
		items := make([]relationshipUserResponse, 0)
		hasMore := false
		for _, user := range users {
			if !strings.Contains(normalizeSearchText(user.Username), query) && !strings.Contains(normalizeSearchText(user.DisplayName), query) {
				continue
			}
			if blocked[user.ID] {
				continue
			}
			if len(items) == 50 {
				hasMore = true
				break
			}
			if err := user.setImageKey(); err != nil {
				return err
			}
			if user.AccountType != "certified_public" {
				user.Bio = ""
			}
			items = append(items, newRelationshipUserResponse(user))
		}
		return c.JSON(http.StatusOK, searchUsersResponse{Items: items, HasMore: hasMore})
	}
}

type getUserRequest struct {
	UserID string `param:"userId"`
}

type getUserResponse relationshipUserResponse

func getUserHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getUserRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		viewer := GetSession(c).UserID
		user, err := getUser(ctx, h.DB, input.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		}
		if err != nil {
			return err
		}
		blocked, err := isBlocked(ctx, h.DB, viewer, user.ID)
		if err != nil {
			return err
		}
		if blocked {
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		}
		friends, err := getFriendIDs(ctx, h.DB, viewer)
		if err != nil {
			return err
		}
		if user.AccountType != "certified_public" && !slices.Contains(friends, user.ID) {
			user.Bio = ""
		}
		return c.JSON(http.StatusOK, getUserResponse(newRelationshipUserResponse(user)))
	}
}

func newRelationshipUserResponse(user User) relationshipUserResponse {
	return relationshipUserResponse{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, ProfileImageURL: newGetMeResponse(user).ProfileImageURL, Bio: user.Bio, AccountType: user.AccountType}
}
