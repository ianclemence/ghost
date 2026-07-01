package channels

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/logger"
)

type WeChatChannel struct {
	*BaseChannel
	config     config.WeChatConfig
	httpClient *http.Client
	server     *http.Server
	tokenCache *tokenCache
	mu         sync.Mutex
}

type tokenCache struct {
	token     string
	expiresAt time.Time
	mu        sync.RWMutex
}

type wechatXMLMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgId        int64    `xml:"MsgId"`
}

type wechatXMLResponse struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
}

func NewWeChatChannel(cfg config.WeChatConfig, bus *bus.MessageBus) (*WeChatChannel, error) {
	if cfg.CorpID == "" || cfg.Secret == "" {
		return nil, fmt.Errorf("WeChat requires corp_id and secret")
	}

	if cfg.AgentID == "" {
		return nil, fmt.Errorf("WeChat requires agent_id")
	}

	base := NewBaseChannel("wechat", cfg, bus, cfg.AllowFrom)

	return &WeChatChannel{
		BaseChannel: base,
		config:      cfg,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		tokenCache:  &tokenCache{},
	}, nil
}

func (c *WeChatChannel) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/wechat/callback", c.handleCallback)

	c.server = &http.Server{
		Addr:    ":8082",
		Handler: mux,
	}

	go func() {
		logger.InfoC("wechat", "Starting callback server on :8082")
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.ErrorCF("wechat", "Callback server error: %v", map[string]interface{}{"error": err.Error()})
		}
	}()

	c.setRunning(true)
	logger.InfoC("wechat", "Channel started")
	return nil
}

func (c *WeChatChannel) Stop(ctx context.Context) error {
	c.setRunning(false)

	if c.server != nil {
		if err := c.server.Shutdown(ctx); err != nil {
			logger.ErrorCF("wechat", "Error stopping callback server: %v", map[string]interface{}{"error": err.Error()})
		}
	}

	logger.InfoC("wechat", "Channel stopped")
	return nil
}

func (c *WeChatChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("wechat channel is not running")
	}

	toUserID := msg.ChatID
	if toUserID == "" {
		return fmt.Errorf("no recipient specified")
	}

	token, err := c.getAccessToken()
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	return c.sendTextMessage(ctx, token, toUserID, msg.Content)
}

func (c *WeChatChannel) handleCallback(w http.ResponseWriter, r *http.Request) {
	msgSignature := r.URL.Query().Get("msg_signature")
	timestamp := r.URL.Query().Get("timestamp")
	nonce := r.URL.Query().Get("nonce")
	echostr := r.URL.Query().Get("echostr")

	if r.Method == http.MethodGet {
		if c.verifySignature(c.config.Token, timestamp, nonce, msgSignature) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, echostr)
		} else {
			http.Error(w, "Invalid signature", http.StatusForbidden)
		}
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var msg wechatXMLMessage
	if err := xml.Unmarshal(body, &msg); err != nil {
		http.Error(w, "Invalid XML", http.StatusBadRequest)
		return
	}

	if !c.verifySignature(c.config.Token, timestamp, nonce, msgSignature) {
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	logger.InfoCF("wechat", "Received message", map[string]interface{}{
		"from": msg.FromUserName,
		"type": msg.MsgType,
	})

	if msg.MsgType == "text" {
		metadata := map[string]string{
			"message_id": fmt.Sprintf("%d", msg.MsgId),
			"username":   msg.FromUserName,
		}
		c.HandleMessage(msg.FromUserName, msg.FromUserName, msg.Content, nil, metadata)
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, "success")
}

func (c *WeChatChannel) verifySignature(token, timestamp, nonce, msgSignature string) bool {
	strs := []string{token, timestamp, nonce}
	sort.Strings(strs)
	combined := strings.Join(strs, "")

	hash := sha1.New()
	hash.Write([]byte(combined))
	signature := fmt.Sprintf("%x", hash.Sum(nil))

	return signature == msgSignature
}

func (c *WeChatChannel) getAccessToken() (string, error) {
	c.tokenCache.mu.RLock()
	if c.tokenCache.token != "" && time.Now().Before(c.tokenCache.expiresAt) {
		defer c.tokenCache.mu.RUnlock()
		return c.tokenCache.token, nil
	}
	c.tokenCache.mu.RUnlock()

	c.tokenCache.mu.Lock()
	defer c.tokenCache.mu.Unlock()

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		c.config.CorpID, c.config.Secret)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("WeChat API error %d: %s", result.ErrCode, result.ErrMsg)
	}

	c.tokenCache.token = result.AccessToken
	c.tokenCache.expiresAt = time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)

	return result.AccessToken, nil
}

func (c *WeChatChannel) sendTextMessage(ctx context.Context, token, toUser, content string) error {
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)

	payload := map[string]interface{}{
		"touser":  toUser,
		"msgtype": "text",
		"agentid": c.config.AgentID,
		"text": map[string]string{
			"content": content,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("WeChat API error %d: %s", result.ErrCode, result.ErrMsg)
	}

	return nil
}
