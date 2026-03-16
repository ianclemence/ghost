package bus

import (
	"context"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

type outboundSubscriber struct {
	id        int
	name      string
	reliable  bool
	ch        chan OutboundMessage
	bufferCap int
}

type MessageBus struct {
	inbound             chan InboundMessage
	handlers            map[string]MessageHandler
	outboundSubscribers map[int]*outboundSubscriber
	nextSubscriberID    int
	mu                  sync.RWMutex
}

func NewMessageBus() *MessageBus {
	return &MessageBus{
		inbound:             make(chan InboundMessage, 100),
		handlers:            make(map[string]MessageHandler),
		outboundSubscribers: make(map[int]*outboundSubscriber),
	}
}

func (mb *MessageBus) PublishInbound(msg InboundMessage) {
	mb.inbound <- msg
}

func (mb *MessageBus) ConsumeInbound(ctx context.Context) (InboundMessage, bool) {
	select {
	case msg := <-mb.inbound:
		return msg, true
	case <-ctx.Done():
		return InboundMessage{}, false
	}
}

func (mb *MessageBus) PublishOutbound(msg OutboundMessage) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	sessionID, _ := msg.Metadata["session_id"].(string)
	messageID, _ := msg.Metadata["message_id"].(string)
	logger.DebugCF("bus", "Outbound publish", map[string]interface{}{
		"channel":     msg.Channel,
		"chat_id":     msg.ChatID,
		"session_id":  sessionID,
		"message_id":  messageID,
		"subscribers": len(mb.outboundSubscribers),
	})
	for _, sub := range mb.outboundSubscribers {
		start := time.Now()
		if sub.reliable {
			sub.ch <- msg
			elapsed := time.Since(start)
			logger.DebugCF("bus", "Outbound enqueued", map[string]interface{}{
				"subscriber": sub.name,
				"reliable":   true,
				"channel":    msg.Channel,
				"chat_id":    msg.ChatID,
				"session_id": sessionID,
				"message_id": messageID,
				"wait_ms":    elapsed.Milliseconds(),
			})
			if elapsed > 500*time.Millisecond {
				logger.WarnCF("bus", "Reliable outbound enqueue delayed", map[string]interface{}{
					"subscriber": sub.name,
					"wait_ms":    elapsed.Milliseconds(),
					"buffer_cap": sub.bufferCap,
				})
			}
			continue
		}
		select {
		case sub.ch <- msg:
			logger.DebugCF("bus", "Outbound enqueued", map[string]interface{}{
				"subscriber": sub.name,
				"reliable":   false,
				"channel":    msg.Channel,
				"chat_id":    msg.ChatID,
				"session_id": sessionID,
				"message_id": messageID,
			})
		default:
			logger.WarnCF("bus", "Dropping outbound for slow subscriber", map[string]interface{}{
				"subscriber": sub.name,
				"channel":    msg.Channel,
				"chat_id":    msg.ChatID,
				"session_id": sessionID,
				"message_id": messageID,
				"buffer_cap": sub.bufferCap,
			})
		}
	}
}

func (mb *MessageBus) SubscribeOutbound(name string, reliable bool, buffer int) (<-chan OutboundMessage, func()) {
	if buffer <= 0 {
		buffer = 1000
	}
	mb.mu.Lock()
	id := mb.nextSubscriberID
	mb.nextSubscriberID++
	ch := make(chan OutboundMessage, buffer)
	mb.outboundSubscribers[id] = &outboundSubscriber{
		id:        id,
		name:      name,
		reliable:  reliable,
		ch:        ch,
		bufferCap: buffer,
	}
	mb.mu.Unlock()

	logger.InfoCF("bus", "Outbound subscriber registered", map[string]interface{}{
		"subscriber": name,
		"reliable":   reliable,
		"buffer_cap": buffer,
	})

	unsubscribe := func() {
		mb.mu.Lock()
		if sub, ok := mb.outboundSubscribers[id]; ok {
			delete(mb.outboundSubscribers, id)
			close(sub.ch)
			logger.InfoCF("bus", "Outbound subscriber removed", map[string]interface{}{
				"subscriber": sub.name,
			})
		}
		mb.mu.Unlock()
	}
	return ch, unsubscribe
}

func (mb *MessageBus) RegisterHandler(channel string, handler MessageHandler) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.handlers[channel] = handler
}

func (mb *MessageBus) GetHandler(channel string) (MessageHandler, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	handler, ok := mb.handlers[channel]
	return handler, ok
}

func (mb *MessageBus) Close() {
	close(mb.inbound)
	mb.mu.Lock()
	for id, sub := range mb.outboundSubscribers {
		close(sub.ch)
		delete(mb.outboundSubscribers, id)
	}
	mb.mu.Unlock()
}
