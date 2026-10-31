package isureal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

const (
	internalTokenHeader  = "X-Internal-Token"
	maxInternalImageSize = 32 << 20
	imageTransferTimeout = 10 * time.Second
	imageResetTimeout    = 3 * time.Second
)

// private IP の経路はルーターを通る。公開 IP はルーターを迂回するので使わない
var nodeHosts = map[string]string{
	"asia":    "192.168.1.10",
	"asia2":   "192.168.4.10",
	"europe":  "192.168.2.10",
	"america": "192.168.3.10",
}

// リージョンごとのサーバー。通知の接続はリージョンのどのサーバーにもある
var regionNodes = map[string][]string{
	"asia":    {"asia", "asia2"},
	"europe":  {"europe"},
	"america": {"america"},
}

// ImageStore は画像の本体をリージョンごとのディスクに置く。
// new/ は DB に本体が無い新規の画像で、初期化で消す。
// cache/ は DB に本体がある初期画像の控えで、中身が変わらないので初期化でも残す。
type ImageStore struct {
	root   string
	node   string
	token  string
	client *http.Client
	// primary はリージョンの代表ノード (regionNodes の先頭)。それ以外のノード (asia2) は DB と同じホストなので
	// 配布された画像を保存せず、要求のたびに代表ノードから取って返す
	primary bool
}

// primaryNode はそのノードがリージョンの代表 (画像を保存して配る側) かを返す
func primaryNode(node string) bool {
	for _, nodes := range regionNodes {
		for i, candidate := range nodes {
			if candidate == node {
				return i == 0
			}
		}
	}
	return true
}

// regionPrimary はノードが属するリージョンの代表ノードを返す
func regionPrimary(node string) string {
	for _, nodes := range regionNodes {
		for _, candidate := range nodes {
			if candidate == node {
				return nodes[0]
			}
		}
	}
	return node
}

func NewImageStore(config *Config) *ImageStore {
	hash := sha256.Sum256([]byte("isureal-internal:" + config.DBPassword))
	return &ImageStore{
		root:    config.ImageDir,
		node:    config.Node,
		primary: primaryNode(config.Node),
		token:   hex.EncodeToString(hash[:]),
		client: &http.Client{
			Timeout:   imageTransferTimeout,
			Transport: &http.Transport{MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second},
		},
	}
}

func (s *ImageStore) newPath(imageID string) string {
	return filepath.Join(s.root, "new", imageID[:2], imageID)
}

func (s *ImageStore) cachePath(imageID string) string {
	return filepath.Join(s.root, "cache", imageID[:2], imageID)
}

// accelPath は nginx の internal location (/internal-images/ → ImageDir) から見た画像のパス。
// 本体は nginx が sendfile で返し、Go は署名の検証だけを行う
func (s *ImageStore) accelPath(imageID string, onDisk bool) string {
	dir := "cache"
	if onDisk {
		dir = "new"
	}
	return "/internal-images/" + dir + "/" + imageID[:2] + "/" + imageID
}

func (s *ImageStore) hasCached(imageID string) bool {
	_, err := os.Stat(s.cachePath(imageID))
	return err == nil
}

// 途中まで書いたファイルが読まれないよう、一時ファイルに書いて rename する
func writeImageFile(path string, data []byte, durable bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	// nginx (www-data) が X-Accel-Redirect で直接読めるようにする
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if durable {
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SaveNew は再起動後も読めるよう fsync してから戻る。ロックを持つ時間を延ばさないよう、トランザクションの前に呼ぶ。
// 代表でないノードは配布先の控えがあるので fsync しない
func (s *ImageStore) SaveNew(imageID string, data []byte) error {
	return writeImageFile(s.newPath(imageID), data, s.primary)
}

// LoadNew は手元に無ければ保存元のリージョンから取得する。配布が届く前の読み取りはここで救う
func (s *ImageStore) LoadNew(ctx context.Context, imageID string, originRegion *string) ([]byte, error) {
	path := s.newPath(imageID)
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if originRegion == nil || *originRegion == s.node {
		return nil, fmt.Errorf("image %s is missing", imageID)
	}
	if !s.primary {
		// 保存せず、同一リージョンの代表ノードから取って返す。届いていなければ保存元へ
		primary := regionPrimary(s.node)
		if primary != *originRegion {
			if data, err := s.fetchFromRegion(ctx, primary, imageID); err == nil {
				return data, nil
			}
		}
		return s.fetchFromRegion(ctx, *originRegion, imageID)
	}
	data, err = s.fetchFromRegion(ctx, *originRegion, imageID)
	if err != nil {
		return nil, err
	}
	if err := writeImageFile(path, data, true); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *ImageStore) LoadCached(imageID string) ([]byte, bool) {
	data, err := os.ReadFile(s.cachePath(imageID))
	return data, err == nil
}

// SaveCached の控えは DB から作り直せるので、fsync せず、失敗しても応答は止めない
func (s *ImageStore) SaveCached(imageID string, data []byte) {
	if err := writeImageFile(s.cachePath(imageID), data, false); err != nil {
		log.Printf("failed to cache image %s: %v", imageID, err)
	}
}

func (s *ImageStore) nodeURL(node, path string) (string, error) {
	host, ok := nodeHosts[node]
	if !ok {
		return "", fmt.Errorf("unknown node: %s", node)
	}
	return "http://" + host + ":8080" + path, nil
}

func (s *ImageStore) internalRequest(ctx context.Context, method, node, path string, body []byte) (*http.Response, error) {
	url, err := s.nodeURL(node, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set(internalTokenHeader, s.token)
	return s.client.Do(req)
}

func (s *ImageStore) fetchFromRegion(ctx context.Context, node, imageID string) ([]byte, error) {
	res, err := s.internalRequest(ctx, http.MethodGet, node, "/internal/images/"+imageID, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch image %s from %s: %s", imageID, node, res.Status)
	}
	return io.ReadAll(res.Body)
}

// Distribute は裏で他のリージョンへ配る。失敗しても読み取り時に保存元から取得できる
func (s *ImageStore) Distribute(imageID string, data []byte) {
	for node := range nodeHosts {
		if node == s.node || !primaryNode(node) {
			continue
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), imageTransferTimeout)
			defer cancel()
			res, err := s.internalRequest(ctx, http.MethodPut, node, "/internal/images/"+imageID, data)
			if err != nil {
				log.Printf("failed to distribute image %s to %s: %v", imageID, node, err)
				return
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}()
	}
}

// ClearNew は rename してから消すので、大量のファイルがあっても初期化を待たせない
func (s *ImageStore) ClearNew() error {
	dir := filepath.Join(s.root, "new")
	trash := fmt.Sprintf("%s.trash-%d", dir, time.Now().UnixNano())
	if err := os.Rename(dir, trash); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	go func() {
		if err := os.RemoveAll(trash); err != nil {
			log.Printf("failed to remove %s: %v", trash, err)
		}
	}()
	return nil
}

// ClearNewInAllRegions の残骸は DB に行が無く読み出せないので、失敗しても初期化は止めない
func (s *ImageStore) ClearNewInAllRegions() {
	if err := s.ClearNew(); err != nil {
		log.Printf("failed to clear images: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), imageResetTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for node := range nodeHosts {
		if node == s.node {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.internalRequest(ctx, http.MethodPost, node, "/internal/images-reset", nil)
			if err != nil {
				log.Printf("failed to clear images in %s: %v", node, err)
				return
			}
			res.Body.Close()
		}()
	}
	wg.Wait()
}

// 画像 ID はパスに使うので、UUID の形だけを受け付ける
func validImageID(imageID string) bool {
	parsed, err := uuid.Parse(imageID)
	return err == nil && parsed.String() == imageID
}

func InternalOnlyMiddleware(store *ImageStore) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			token := c.Request().Header.Get(internalTokenHeader)
			if subtle.ConstantTimeCompare([]byte(token), []byte(store.token)) != 1 {
				return c.NoContent(http.StatusNotFound)
			}
			return next(c)
		}
	}
}

func getInternalImageHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		imageID := c.Param("imageId")
		if !validImageID(imageID) {
			return c.NoContent(http.StatusNotFound)
		}
		data, err := os.ReadFile(h.Images.newPath(imageID))
		if errors.Is(err, fs.ErrNotExist) {
			return c.NoContent(http.StatusNotFound)
		}
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "application/octet-stream", data)
	}
}

func putInternalImageHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		imageID := c.Param("imageId")
		if !validImageID(imageID) {
			return c.NoContent(http.StatusNotFound)
		}
		data, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, maxInternalImageSize))
		if err != nil {
			return err
		}
		if !h.Images.primary {
			return c.NoContent(http.StatusNoContent)
		}
		if err := h.Images.SaveNew(imageID, data); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func resetInternalImagesHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if err := h.Images.ClearNew(); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}
