package bus

import (
	"context"
	"sync"
)

type MessageBus struct {
	inbound             chan InboundMessage
	handlers            map[string]MessageHandler
	outboundSubscribers map[int]chan OutboundMessage
	nextSubscriberID    int
	mu                  sync.RWMutex
}

func NewMessageBus() *MessageBus {
	return &MessageBus{
		inbound:             make(chan InboundMessage, 100),
		handlers:            make(map[string]MessageHandler),
		outboundSubscribers: make(map[int]chan OutboundMessage),
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
	for _, ch := range mb.outboundSubscribers {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (mb *MessageBus) SubscribeOutbound() (<-chan OutboundMessage, func()) {
	mb.mu.Lock()
	id := mb.nextSubscriberID
	mb.nextSubscriberID++
	ch := make(chan OutboundMessage, 100)
	mb.outboundSubscribers[id] = ch
	mb.mu.Unlock()

	unsubscribe := func() {
		mb.mu.Lock()
		if sub, ok := mb.outboundSubscribers[id]; ok {
			delete(mb.outboundSubscribers, id)
			close(sub)
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
	for id, ch := range mb.outboundSubscribers {
		close(ch)
		delete(mb.outboundSubscribers, id)
	}
	mb.mu.Unlock()
}
