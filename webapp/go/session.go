package isureal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
)

type Session struct {
	Region    string    `db:"region" json:"-"`
	Token     string    `db:"-" json:"-"`
	UserID    string    `db:"user_id"`
	TokenHash string    `db:"token_hash"`
	CreatedAt time.Time `db:"created_at"`
}

func GetSession(c *echo.Context) *Session {
	sess, ok := c.Get("session").(*Session)
	if !ok {
		return nil
	}
	return sess
}

// リージョンにアプリサーバーが 1 台しかない europe / america でだけ、セッションを手元に持つ。
// DB までの 1 往復 (100 ms) が全リクエストに乗っているため。ログイン・ログアウト・退会・リージョン変更は
// 同じサーバーに来るので、その場で消せば整合が保てる。asia は asia1 / asia2 に振り分けられるので使わない
const sessionCacheTTL = 2 * time.Second

type sessionCache struct {
	enabled bool
	mu      sync.RWMutex
	entries map[string]sessionEntry
}

type sessionEntry struct {
	session   Session
	fetchedAt time.Time
}

var sessions = &sessionCache{entries: map[string]sessionEntry{}}

func (c *sessionCache) get(tokenHash string) (*Session, bool) {
	if !c.enabled {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[tokenHash]
	if !ok || time.Since(entry.fetchedAt) >= sessionCacheTTL {
		return nil, false
	}
	session := entry.session
	return &session, true
}

func (c *sessionCache) put(session Session) {
	if !c.enabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[session.TokenHash] = sessionEntry{session: session, fetchedAt: time.Now()}
}

func (c *sessionCache) forgetToken(tokenHash string) {
	if !c.enabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, tokenHash)
}

func (c *sessionCache) forgetUser(userID string) {
	if !c.enabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for tokenHash, entry := range c.entries {
		if entry.session.UserID == userID {
			delete(c.entries, tokenHash)
		}
	}
}

func getSessionFromHeader(c *echo.Context, db *sqlx.DB) (*Session, error) {
	authHeader := c.Request().Header.Get(echo.HeaderAuthorization)
	if authHeader == "" {
		return nil, nil
	}
	if !strings.HasPrefix(authHeader, "Bearer ") {
		c.Logger().Warn("Invalid authorization header format(not started with Bearer)")
		return nil, NewAPIError(http.StatusUnauthorized, "invalid_session")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")

	tokenHashBytes := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(tokenHashBytes[:])
	if session, ok := sessions.get(tokenHash); ok {
		return session, nil
	}
	var session Session
	err := db.GetContext(c.Request().Context(), &session, `
		SELECT s.user_id, s.token_hash, s.created_at, u.region
		FROM login_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
		  AND s.active = true AND u.status = 'active'
	`, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	sessions.put(session)
	return &session, nil
}

func SessionMiddleware(handlerContext *HandlerContext, authRequired bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			session, err := getSessionFromHeader(c, handlerContext.DB)
			if err != nil {
				return err
			}
			if session == nil && authRequired {
				return NewAPIError(http.StatusUnauthorized, "invalid_session")
			}
			c.Set("session", session)
			return next(c)
		}
	}
}

func InvalidateLoginSession(db *sqlx.DB, userID string, tokenHash string) error {
	if _, err := db.Exec(`
		UPDATE login_sessions
		SET active = false, invalidated_at = clock_timestamp(), reason = 'logout'
		WHERE user_id = $1 AND active = true AND token_hash = $2
	`, userID, tokenHash); err != nil {
		return fmt.Errorf("failed to invalidate sessions: %w", err)
	}
	sessions.forgetToken(tokenHash)
	return nil
}

// 2026-09-03: リファクタリング後にこの関数の利用がないようです。消すのはいかがでしょうか
func CreateLoginSession(db *sqlx.DB, userID string) (*Session, error) {
	ctx := context.Background()
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	session, err := createLoginSessionTx(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return session, nil
}

func createLoginSessionTx(ctx context.Context, tx *sqlx.Tx, userID string) (*Session, error) {
	randomToken := uuid.NewString()
	tokenHashBytes := sha256.Sum256([]byte(randomToken))
	tokenHash := hex.EncodeToString(tokenHashBytes[:])
	if err := lockUserSlot(ctx, tx, "session", userID); err != nil {
		return nil, fmt.Errorf("failed to lock user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
  UPDATE login_sessions
  SET active = false, invalidated_at = clock_timestamp(), reason = 'replaced'
  WHERE user_id = $1 AND active = true
 `, userID); err != nil {
		return nil, fmt.Errorf("failed to invalidate sessions: %w", err)
	}
	sessionID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
  INSERT INTO login_sessions (id, user_id, token_hash)
  VALUES ($1, $2, $3)
 `, sessionID, userID, tokenHash); err != nil {
		return nil, fmt.Errorf("failed to insert session: %w", err)
	}
	session, err := getLoginSession(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	session.Token = randomToken
	return session, nil
}

func getLoginSession(ctx context.Context, db sqlx.QueryerContext, sessionID string) (*Session, error) {
	var session Session
	if err := sqlx.GetContext(ctx, db, &session, `SELECT user_id, token_hash, created_at FROM login_sessions WHERE id = $1`, sessionID); err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	return &session, nil
}
