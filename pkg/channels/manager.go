// Ghost - Ultra-lightweight personal AI agent
// Inspired by and based on GHOST: https://github.com/ianclemence/ghost
// License: MIT
//
// Copyright (c) 2026 Ghost contributors

package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/commands"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/constants"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/telemetry"
	"github.com/ianclemence/ghost/pkg/tools"
)

type Manager struct {
	channels         map[string]Channel
	outboundQueues   map[string]chan bus.OutboundMessage
	bus              *bus.MessageBus
	config           *config.Config
	dispatchTask     *asyncTask
	workerTasks      map[string]*asyncTask
	failureCount     map[string]int
	fatalChannels    map[string]string
	lastSendError    map[string]string
	lastFailureAt    map[string]int64
	lastSuccessAt    map[string]int64
	deliveryRouter   *DeliveryRouter
	commandDefs      []commands.Definition
	deliveryObserver func(msg bus.OutboundMessage, target string, ok bool, errText string)
	mu               sync.RWMutex
}

type CommandDefinitionsSetter interface {
	SetCommandDefinitions(defs []commands.Definition)
}

type asyncTask struct {
	cancel context.CancelFunc
}

var (
	channelToolProfiles = map[string]tools.ToolProfile{
		"mobile":    tools.ProfileMobileSafe,
		"heartbeat": tools.ProfileHeartbeatSafe,
		"cron":      tools.ProfileHeartbeatSafe,
	}
	sessionToolProfiles = map[string]tools.ToolProfile{}
	toolPolicyMu        sync.RWMutex
)

func SetChannelToolProfile(channel string, profile tools.ToolProfile) {
	toolPolicyMu.Lock()
	defer toolPolicyMu.Unlock()
	channelToolProfiles[strings.ToLower(strings.TrimSpace(channel))] = profile
}

func SetSessionToolProfile(sessionKey string, profile tools.ToolProfile) {
	toolPolicyMu.Lock()
	defer toolPolicyMu.Unlock()
	sessionToolProfiles[strings.TrimSpace(sessionKey)] = profile
}

func DetectToolProfile(channel, clientType, sessionKey string, isHeartbeat bool) tools.ToolProfile {
	toolPolicyMu.RLock()
	if profile, ok := sessionToolProfiles[strings.TrimSpace(sessionKey)]; ok {
		toolPolicyMu.RUnlock()
		return profile
	}
	toolPolicyMu.RUnlock()

	if isHeartbeat {
		return tools.ProfileHeartbeatSafe
	}

	if strings.EqualFold(strings.TrimSpace(clientType), "mobile") {
		return tools.ProfileMobileSafe
	}

	normalized := strings.ToLower(strings.TrimSpace(channel))
	toolPolicyMu.RLock()
	defer toolPolicyMu.RUnlock()
	if profile, ok := channelToolProfiles[normalized]; ok {
		return profile
	}
	return tools.ProfileFull
}

func NewManager(cfg *config.Config, messageBus *bus.MessageBus, sp ActiveSessionProvider) (*Manager, error) {
	m := &Manager{
		channels:       make(map[string]Channel),
		outboundQueues: make(map[string]chan bus.OutboundMessage),
		workerTasks:    make(map[string]*asyncTask),
		failureCount:   make(map[string]int),
		fatalChannels:  make(map[string]string),
		lastSendError:  make(map[string]string),
		lastFailureAt:  make(map[string]int64),
		lastSuccessAt:  make(map[string]int64),
		bus:            messageBus,
		config:         cfg,
		deliveryRouter: NewDeliveryRouter(sp),
	}

	if err := m.initChannels(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) initChannels() error {
	logger.InfoC("channels", "Initializing channel manager")

	if m.config.Channels.Telegram.Enabled && m.config.Channels.Telegram.Token != "" {
		logger.DebugC("channels", "Attempting to initialize Telegram channel")
		telegram, err := NewTelegramChannel(m.config.Channels.Telegram, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize Telegram channel", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			m.channels["telegram"] = telegram
			logger.InfoC("channels", "Telegram channel enabled successfully")
		}
	}

	if m.config.Channels.WhatsApp.Enabled && m.config.Channels.WhatsApp.BridgeURL != "" {
		logger.DebugC("channels", "Attempting to initialize WhatsApp channel")
		whatsapp, err := NewWhatsAppChannel(m.config.Channels.WhatsApp, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize WhatsApp channel", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			m.channels["whatsapp"] = whatsapp
			logger.InfoC("channels", "WhatsApp channel enabled successfully")
		}
	}

	if m.config.Channels.Discord.Enabled && m.config.Channels.Discord.Token != "" {
		logger.DebugC("channels", "Attempting to initialize Discord channel")
		discord, err := NewDiscordChannel(m.config.Channels.Discord, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize Discord channel", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			m.channels["discord"] = discord
			logger.InfoC("channels", "Discord channel enabled successfully")
		}
	}

	if m.config.Channels.Slack.Enabled && m.config.Channels.Slack.BotToken != "" && m.config.Channels.Slack.AppToken != "" {
		logger.DebugC("channels", "Attempting to initialize Slack channel")
		slackCh, err := NewSlackChannel(m.config.Channels.Slack, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize Slack channel", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			m.channels["slack"] = slackCh
			logger.InfoC("channels", "Slack channel enabled successfully")
		}
	}

	if m.config.Channels.LINE.Enabled && m.config.Channels.LINE.ChannelAccessToken != "" {
		logger.DebugC("channels", "Attempting to initialize LINE channel")
		line, err := NewLINEChannel(m.config.Channels.LINE, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize LINE channel", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			m.channels["line"] = line
			logger.InfoC("channels", "LINE channel enabled successfully")
		}
	}

	if m.config.Channels.Email.Enabled {
		logger.DebugC("channels", "Attempting to initialize Email channel")
		email, err := NewEmailChannel(m.config.Channels.Email, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize Email channel", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			m.channels["email"] = email
			logger.InfoC("channels", "Email channel enabled successfully")
		}
	}

	logger.InfoCF("channels", "Channel initialization completed", map[string]interface{}{
		"enabled_channels": len(m.channels),
	})

	return nil
}

func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.channels) == 0 {
		logger.WarnC("channels", "No channels enabled")
		return nil
	}

	logger.InfoC("channels", "Starting all channels")

	dispatchCtx, cancel := context.WithCancel(ctx)
	m.dispatchTask = &asyncTask{cancel: cancel}

	go m.dispatchOutbound(dispatchCtx)

	for name, channel := range m.channels {
		logger.InfoCF("channels", "Starting channel", map[string]interface{}{
			"channel": name,
		})
		if len(m.commandDefs) > 0 {
			if setter, ok := channel.(CommandDefinitionsSetter); ok {
				setter.SetCommandDefinitions(m.commandDefs)
			}
		}
		if err := channel.Start(ctx); err != nil {
			logger.ErrorCF("channels", "Failed to start channel", map[string]interface{}{
				"channel": name,
				"error":   err.Error(),
			})
			m.failureCount[name]++
			errText := strings.ToLower(err.Error())
			if strings.Contains(errText, "conflict") || strings.Contains(errText, "unauthorized") || strings.Contains(errText, "forbidden") {
				m.fatalChannels[name] = err.Error()
				logger.ErrorCF("channels", "Channel startup entered fatal state", map[string]interface{}{
					"channel": name,
					"reason":  err.Error(),
				})
			}
			continue
		}
		queue := make(chan bus.OutboundMessage, 200)
		m.outboundQueues[name] = queue
		workerCtx, workerCancel := context.WithCancel(dispatchCtx)
		m.workerTasks[name] = &asyncTask{cancel: workerCancel}
		go m.channelWorker(workerCtx, name, queue)
	}

	logger.InfoC("channels", "All channels started")
	return nil
}

func (m *Manager) SetCommandDefinitions(defs []commands.Definition) {
	m.commandDefs = defs
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger.InfoC("channels", "Stopping all channels")

	if m.dispatchTask != nil {
		m.dispatchTask.cancel()
		m.dispatchTask = nil
	}
	for name, task := range m.workerTasks {
		task.cancel()
		delete(m.workerTasks, name)
	}
	for name, q := range m.outboundQueues {
		close(q)
		delete(m.outboundQueues, name)
	}

	for name, channel := range m.channels {
		logger.InfoCF("channels", "Stopping channel", map[string]interface{}{
			"channel": name,
		})
		if err := channel.Stop(ctx); err != nil {
			logger.ErrorCF("channels", "Error stopping channel", map[string]interface{}{
				"channel": name,
				"error":   err.Error(),
			})
		}
	}

	logger.InfoC("channels", "All channels stopped")
	return nil
}

func (m *Manager) dispatchOutbound(ctx context.Context) {
	logger.InfoC("channels", "Outbound dispatcher started")
	outboundCh, unsubscribe := m.bus.SubscribeOutbound("channels-dispatcher", true, 2000)
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			logger.InfoC("channels", "Outbound dispatcher stopped")
			return
		case msg, ok := <-outboundCh:
			if !ok {
				return
			}
			sessionID, _ := msg.Metadata["session_id"].(string)
			messageID, _ := msg.Metadata["message_id"].(string)
			logger.DebugCF("channels", "Outbound dequeued", map[string]interface{}{
				"channel":    msg.Channel,
				"chat_id":    msg.ChatID,
				"session_id": sessionID,
				"message_id": messageID,
			})

			// Silently skip internal channels
			if constants.IsInternalChannel(msg.Channel) {
				continue
			}

			target := m.deliveryRouter.ResolveTarget(msg)
			m.mu.RLock()
			q, exists := m.outboundQueues[target]
			reason := m.fatalChannels[target]
			m.mu.RUnlock()
			if reason != "" {
				logger.ErrorCF("channels", "Skipping outbound for fatal channel", map[string]interface{}{
					"channel": target,
					"reason":  reason,
				})
				continue
			}
			if !exists {
				logger.WarnCF("channels", "Unknown channel for outbound message", map[string]interface{}{
					"channel": target,
				})
				continue
			}
			select {
			case q <- msg:
			default:
				logger.WarnCF("channels", "Outbound queue full; dropping oldest then enqueue", map[string]interface{}{
					"channel": target,
				})
				select {
				case <-q:
				default:
				}
				q <- msg
			}
		}
	}
}

func (m *Manager) channelWorker(ctx context.Context, name string, queue <-chan bus.OutboundMessage) {
	backoff := 200 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-queue:
			if !ok {
				return
			}
			m.mu.RLock()
			channel := m.channels[name]
			m.mu.RUnlock()
			if channel == nil {
				continue
			}
			var sendErr error
			for attempt := 1; attempt <= 3; attempt++ {
				sendErr = channel.Send(ctx, msg)
				if sendErr == nil {
					break
				}
				time.Sleep(backoff * time.Duration(attempt))
			}
			if sendErr != nil {
				logger.ErrorCF("channels", "Channel worker send failed after retries", map[string]interface{}{
					"channel": name,
					"error":   sendErr.Error(),
				})
				m.markChannelFailure(name, sendErr)
				m.mu.RLock()
				observer := m.deliveryObserver
				m.mu.RUnlock()
				if observer != nil {
					observer(msg, name, false, sendErr.Error())
				}
				// Also record to global telemetry if request ID is present
				if reqID, _ := msg.Metadata["request_id"].(string); reqID != "" {
					session, _ := msg.Metadata["session_id"].(string)
					telemetry.Global.Record(session, reqID, "delivery_failed", name, msg.ChatID, sendErr.Error())
					telemetry.Global.RecordIncident(name, sendErr.Error())
				}
			} else {
				m.markChannelSuccess(name)
				m.mu.RLock()
				observer := m.deliveryObserver
				m.mu.RUnlock()
				if observer != nil {
					observer(msg, name, true, "")
				}
				// Also record to global telemetry if request ID is present
				if reqID, _ := msg.Metadata["request_id"].(string); reqID != "" {
					session, _ := msg.Metadata["session_id"].(string)
					telemetry.Global.Record(session, reqID, "channel_delivery", name, msg.ChatID, "")
					telemetry.Global.ClearIncidents(name)
				}
			}
		}
	}
}

func (m *Manager) markChannelFailure(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureCount[name]++
	m.lastSendError[name] = err.Error()
	m.lastFailureAt[name] = time.Now().Unix()
	if m.failureCount[name] >= 5 {
		m.fatalChannels[name] = err.Error()
		logger.ErrorCF("channels", "Channel entered fatal state", map[string]interface{}{
			"channel": name,
			"reason":  err.Error(),
		})
	}
}

func (m *Manager) markChannelSuccess(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureCount[name] = 0
	m.lastSuccessAt[name] = time.Now().Unix()
	delete(m.fatalChannels, name)
}

func (m *Manager) GetChannel(name string) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	channel, ok := m.channels[name]
	return channel, ok
}

func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]interface{})
	for name, channel := range m.channels {
		fatalReason := m.fatalChannels[name]
		status[name] = map[string]interface{}{
			"enabled": true,
			"running": channel.IsRunning(),
			"fatal":   fatalReason != "",
			"reason":  fatalReason,
		}
	}
	return status
}

func (m *Manager) GetEnabledChannels() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

func (m *Manager) SetDeliveryObserver(observer func(msg bus.OutboundMessage, target string, ok bool, errText string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliveryObserver = observer
}

func (m *Manager) ResolveTarget(msg bus.OutboundMessage) string {
	return m.deliveryRouter.ResolveTarget(msg)
}

func (m *Manager) RestartChannel(ctx context.Context, name string) error {
	target := strings.ToLower(strings.TrimSpace(name))
	m.mu.RLock()
	ch, ok := m.channels[target]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("channel %s not found", target)
	}
	_ = ch.Stop(ctx)
	if err := ch.Start(ctx); err != nil {
		m.markChannelFailure(target, err)
		return err
	}
	m.markChannelSuccess(target)
	return nil
}

func (m *Manager) GetOperationalStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	known := []string{"telegram", "slack", "discord", "line", "whatsapp", "email"}
	result := make(map[string]interface{}, len(known))
	for _, name := range known {
		enabled := false
		switch name {
		case "telegram":
			enabled = m.config.Channels.Telegram.Enabled
		case "slack":
			enabled = m.config.Channels.Slack.Enabled
		case "discord":
			enabled = m.config.Channels.Discord.Enabled
		case "line":
			enabled = m.config.Channels.LINE.Enabled
		case "whatsapp":
			enabled = m.config.Channels.WhatsApp.Enabled
		case "email":
			enabled = m.config.Channels.Email.Enabled
		}
		ch := m.channels[name]
		running := false
		if ch != nil {
			running = ch.IsRunning()
		}
		result[name] = map[string]interface{}{
			"enabled":         enabled,
			"running":         running,
			"fatal":           m.fatalChannels[name] != "",
			"fatal_reason":    m.fatalChannels[name],
			"failure_count":   m.failureCount[name],
			"last_send_error": m.lastSendError[name],
			"last_failure_at": m.lastFailureAt[name],
			"last_success_at": m.lastSuccessAt[name],
		}
	}
	return result
}

func (m *Manager) RegisterChannel(name string, channel Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[name] = channel
}

func (m *Manager) UnregisterChannel(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, name)
}

func (m *Manager) SendToChannel(ctx context.Context, channelName, chatID, content string) error {
	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("channel %s not found", channelName)
	}

	msg := bus.OutboundMessage{
		Channel: channelName,
		ChatID:  chatID,
		Content: content,
	}

	return channel.Send(ctx, msg)
}

func (m *Manager) SendRouted(ctx context.Context, chatID, content, originChannel string, metadata map[string]interface{}) error {
	msg := bus.OutboundMessage{
		Channel:  originChannel,
		ChatID:   chatID,
		Content:  content,
		Metadata: metadata,
	}
	target := m.deliveryRouter.ResolveTarget(msg)
	return m.SendToChannel(ctx, target, chatID, content)
}
