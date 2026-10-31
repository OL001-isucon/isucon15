package isureal

import (
	"context"
	"log"
	"net/http"
	"sync"

	"github.com/labstack/echo/v5"
)

// NotificationHub は通知の接続を、確認が必要になったときだけ起こす。
// 通知が増えるのは投稿期間の開始時だけなので、接続ごとに DB を問い合わせ続けない
type NotificationHub struct {
	mu      sync.Mutex
	changed chan struct{}
}

func NewNotificationHub() *NotificationHub {
	return &NotificationHub{changed: make(chan struct{})}
}

// Changed は次に Wake されると閉じる channel を返す。DB を確認する前に取得すること
func (h *NotificationHub) Changed() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.changed
}

func (h *NotificationHub) Wake() {
	h.mu.Lock()
	defer h.mu.Unlock()
	close(h.changed)
	h.changed = make(chan struct{})
}

// wakeNotifications は指定のリージョンの全サーバーの接続を起こす。届かなくても定期の確認で拾える
func (h *HandlerContext) wakeNotifications(region string) {
	for _, node := range regionNodes[region] {
		if node == h.Config.Node {
			h.Notifications.Wake()
			continue
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), imageTransferTimeout)
			defer cancel()
			res, err := h.Images.internalRequest(ctx, http.MethodPost, node, "/internal/notifications-wake", nil)
			if err != nil {
				log.Printf("failed to wake notifications in %s: %v", node, err)
				return
			}
			res.Body.Close()
		}()
	}
}

func wakeInternalNotificationsHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		h.Notifications.Wake()
		return c.NoContent(http.StatusNoContent)
	}
}
