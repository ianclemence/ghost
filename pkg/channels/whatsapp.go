package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/utils"
	"github.com/ianclemence/ghost/pkg/voice"
)

type WhatsAppChannel struct {
	*BaseChannel
	conn        *websocket.Conn
	config      config.WhatsAppConfig
	url         string
	mu          sync.Mutex
	connected   bool
	transcriber voice.Transcriber
}

// SetTranscriber attaches speech-to-text for voice notes.
func (c *WhatsAppChannel) SetTranscriber(transcriber voice.Transcriber) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transcriber = transcriber
}

func NewWhatsAppChannel(cfg config.WhatsAppConfig, bus *bus.MessageBus) (*WhatsAppChannel, error) {
	if cfg.BridgeURL == "" {
		return nil, fmt.Errorf("whatsapp bridge_url is required")
	}
	if _, err := url.ParseRequestURI(cfg.BridgeURL); err != nil {
		return nil, fmt.Errorf("invalid whatsapp bridge_url: %w", err)
	}
	base := NewBaseChannel("whatsapp", cfg, bus, cfg.AllowFrom)

	return &WhatsAppChannel{
		BaseChannel: base,
		config:      cfg,
		url:         cfg.BridgeURL,
		connected:   false,
	}, nil
}

func (c *WhatsAppChannel) Start(ctx context.Context) error {
	log.Printf("Starting WhatsApp channel connecting to %s...", c.url)

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WhatsApp bridge: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	c.setRunning(true)
	log.Println("WhatsApp channel connected")

	go c.listen(ctx)

	return nil
}

func (c *WhatsAppChannel) Stop(ctx context.Context) error {
	log.Println("Stopping WhatsApp channel...")

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			log.Printf("Error closing WhatsApp connection: %v", err)
		}
		c.conn = nil
	}

	c.connected = false
	c.setRunning(false)

	return nil
}

func (c *WhatsAppChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("whatsapp connection not established")
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(8 * time.Second))

	payload := map[string]interface{}{
		"type":    "message",
		"to":      msg.ChatID,
		"content": msg.Content,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func (c *WhatsAppChannel) listen(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				time.Sleep(1 * time.Second)
				continue
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WhatsApp read error: %v", err)
				c.mu.Lock()
				c.connected = false
				c.mu.Unlock()
				if recErr := c.reconnect(ctx); recErr != nil {
					log.Printf("WhatsApp reconnect failed: %v", recErr)
					time.Sleep(2 * time.Second)
				}
				continue
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("Failed to unmarshal WhatsApp message: %v", err)
				continue
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			if msgType == "message" {
				c.handleIncomingMessage(msg)
			}
		}
	}
}

func (c *WhatsAppChannel) reconnect(ctx context.Context) error {
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	conn, _, err := dialer.Dial(c.url, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()
	log.Printf("WhatsApp reconnected to %s", c.url)
	return nil
}

func (c *WhatsAppChannel) handleIncomingMessage(msg map[string]interface{}) {
	senderID, ok := msg["from"].(string)
	if !ok {
		return
	}

	chatID, ok := msg["chat"].(string)
	if !ok {
		chatID = senderID
	}

	content, ok := msg["content"].(string)
	if !ok {
		content = ""
	}

	var mediaPaths []string
	var voiceTexts []string
	if mediaData, ok := msg["media"].([]interface{}); ok {
		mediaPaths = make([]string, 0, len(mediaData))
		for _, m := range mediaData {
			entry, ok := m.(string)
			if !ok || entry == "" {
				continue
			}
			localPath, isAudio := c.resolveMediaEntry(entry)
			if localPath == "" {
				mediaPaths = append(mediaPaths, entry)
				continue
			}
			mediaPaths = append(mediaPaths, localPath)
			if isAudio {
				voiceTexts = append(voiceTexts, transcribeVoiceFile(context.Background(), c.transcriber, "whatsapp", localPath, "voice"))
			}
		}
	}
	if len(voiceTexts) > 0 {
		if content != "" {
			content += "\n"
		}
		content += strings.Join(voiceTexts, "\n")
	}

	metadata := make(map[string]string)
	if messageID, ok := msg["id"].(string); ok {
		metadata["message_id"] = messageID
	}
	if userName, ok := msg["from_name"].(string); ok {
		metadata["user_name"] = userName
	}

	log.Printf("WhatsApp message from %s: %s...", senderID, utils.Truncate(content, 50))

	c.HandleMessage(senderID, chatID, content, mediaPaths, metadata)
}

// resolveMediaEntry turns a bridge media entry into a local file. Entries
// are either http(s) URLs (downloaded) or paths on this host (used
// directly). whatsapp-web.js voice notes arrive as opus-in-ogg, so only
// audio-looking entries are flagged for transcription; anything else
// passes through untouched.
func (c *WhatsAppChannel) resolveMediaEntry(entry string) (string, bool) {
	if strings.HasPrefix(entry, "http://") || strings.HasPrefix(entry, "https://") {
		name := path.Base(strings.SplitN(entry, "?", 2)[0])
		if name == "" || name == "." || name == "/" {
			name = "voice-note"
		}
		contentType := ""
		if filepath.Ext(name) == "" {
			contentType = headContentType(entry)
			if ext := audioExtensionForType(contentType); ext != "" {
				name += ext
			}
		}
		if !utils.IsAudioFile(name, contentType) {
			return "", false
		}
		local := utils.DownloadFile(entry, name, utils.DownloadOptions{LoggerPrefix: "whatsapp"})
		if local == "" {
			return "", false
		}
		return local, true
	}
	if st, err := os.Stat(entry); err != nil || st.IsDir() {
		return "", false
	}
	return entry, utils.IsAudioFile(entry, "")
}

func headContentType(rawURL string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	return resp.Header.Get("Content-Type")
}
