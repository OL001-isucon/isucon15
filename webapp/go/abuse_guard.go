package isureal

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
)

const (
	abuseBanDuration = 2 * time.Minute
	abuseBanTimeout  = 3 * time.Second

	loginFailureThreshold = 10
	loginFailureWindow    = 10 * time.Second
	invalidImageThreshold = 2
	invalidImageWindow    = 5 * time.Second
	invalidSigThreshold   = 3
	invalidSigWindow      = 5 * time.Second
)

// abuseDetector は IP ごとに、集計期間内の失敗の時刻を持つ。
type abuseDetector struct {
	threshold int
	window    time.Duration
	failures  map[string][]time.Time
	lastSweep time.Time
}

func newAbuseDetector(threshold int, window time.Duration) *abuseDetector {
	return &abuseDetector{threshold: threshold, window: window, failures: make(map[string][]time.Time)}
}

// observe は失敗を記録し、BAN すべきなら true を返す。BAN したら数え直す
func (d *abuseDetector) observe(ip string, at time.Time) bool {
	d.sweep(at)
	kept := d.failures[ip][:0]
	for _, failedAt := range d.failures[ip] {
		if at.Sub(failedAt) < d.window {
			kept = append(kept, failedAt)
		}
	}
	kept = append(kept, at)
	if len(kept) >= d.threshold {
		delete(d.failures, ip)
		return true
	}
	d.failures[ip] = kept
	return false
}

// sweep は集計期間ごとに、期間内の失敗がなくなった IP を捨てる
func (d *abuseDetector) sweep(now time.Time) {
	if now.Sub(d.lastSweep) < d.window {
		return
	}
	d.lastSweep = now
	for ip, times := range d.failures {
		if now.Sub(times[len(times)-1]) >= d.window {
			delete(d.failures, ip)
		}
	}
}

// AbuseGuard は不正なアクセスを数えて BAN する。
// 元の isureal-guard はアプリのログを読んでいたが、ログに依存しないようアプリの中で判定する
type AbuseGuard struct {
	db        *sqlx.DB
	mu        sync.Mutex
	login     *abuseDetector
	image     *abuseDetector
	signature *abuseDetector
}

func NewAbuseGuard(db *sqlx.DB) *AbuseGuard {
	return &AbuseGuard{
		db:        db,
		login:     newAbuseDetector(loginFailureThreshold, loginFailureWindow),
		image:     newAbuseDetector(invalidImageThreshold, invalidImageWindow),
		signature: newAbuseDetector(invalidSigThreshold, invalidSigWindow),
	}
}

func (g *AbuseGuard) detectorFor(action string, status int, abuse string) *abuseDetector {
	switch {
	case action == "login" && status == http.StatusUnauthorized:
		return g.login
	case action == "put_profile_image" && status == http.StatusBadRequest && abuse == "invalid_image":
		return g.image
	case action == "get_image" && status == http.StatusForbidden && abuse == "invalid_signature":
		return g.signature
	default:
		return nil
	}
}

// Observe は応答ごとに呼ぶ。ほとんどの応答は対象外なので、先に状態コードで絞る
func (g *AbuseGuard) Observe(c *echo.Context, status int) {
	if status != http.StatusUnauthorized && status != http.StatusBadRequest && status != http.StatusForbidden {
		return
	}
	route := c.RouteInfo()
	abuse, _ := c.Get("abuse").(string)
	detector := g.detectorFor(actionNames[route.Method+" "+route.Path], status, abuse)
	if detector == nil {
		return
	}
	ip := c.RealIP()
	if ip == "" {
		return
	}
	g.mu.Lock()
	banned := detector.observe(ip, time.Now())
	g.mu.Unlock()
	if banned {
		go g.ban(ip)
	}
}

// ban は BAN の期限を DB の時刻で決める。すでに BAN されていれば遅い方の期限を残す
func (g *AbuseGuard) ban(ip string) {
	ctx, cancel := context.WithTimeout(context.Background(), abuseBanTimeout)
	defer cancel()
	_, err := g.db.ExecContext(ctx, `
   INSERT INTO ip_bans (ip, banned_until)
   VALUES ($1, clock_timestamp() + make_interval(secs => $2))
   ON CONFLICT (ip) DO UPDATE SET banned_until = GREATEST(ip_bans.banned_until, EXCLUDED.banned_until)
  `, ip, abuseBanDuration.Seconds())
	if err != nil {
		log.Printf("failed to ban %s: %v", ip, err)
	}
}
