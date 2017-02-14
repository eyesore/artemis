package artemis

// Family is group of Agents and AgentDelegates (both Message and Event type).
// Families can subscribe all of their members to handle messages and/or events.
// The family is "dumb" - no handling happens here.
type Family struct {
	Hub *Hub

	Messages messageSubscriber
	Events   eventSubscriber
}

// NewFamily creates a new instance of Family and adds it to a hub.
func NewFamily() *Family {
	return DefaultHub().NewFamily()
}

func (f *Family) Add(d Delegate) {
	f.Messages.Add(d)
	f.Events.Add(d)
}

func (f *Family) Remove(d Delegate) {
	f.Messages.Remove(d)
	f.Events.Remove(d)
}

// PushMessage implements MessagePusher
func (f *Family) PushMessage(m []byte, messageType int) {
	for d := range f.Messages.subscribers {
		d.MessageAgent().PushMessage(m, messageType)
	}
}

func (f *Family) hasMember(d Delegate) bool {
	return f.Events.hasMember(d) || f.Messages.hasMember(d)
}

type messageSubscriber struct {
	subscribers   map[MessageDelegate]struct{}
	subscriptions map[string]MessageHandlerSet
}

func (ms *messageSubscriber) Add(d MessageDelegate) {
	if _, ok := ms.subscribers[d]; ok {
		warn(ErrDuplicateDelegate)
		return
	}
	agent := d.MessageAgent()
	for kind, handlers := range ms.subscriptions {
		for _, h := range handlers {
			agent.Subscribe(kind, h)
		}
	}
	ms.subscribers[d] = struct{}{}
}

func (ms *messageSubscriber) Remove(d MessageDelegate) {
	if _, ok := ms.subscribers[d]; !ok {
		warn(ErrNoDelegates)
		return
	}
	agent := d.MessageAgent()
	for kind, handlers := range ms.subscriptions {
		for _, h := range handlers {
			agent.Unsubscribe(kind, h)
		}
	}
	delete(ms.subscribers, d)
}

func (ms *messageSubscriber) Subscribe(kind string, do MessageHandler) {
	if _, ok := ms.subscriptions[kind]; !ok {
		ms.subscriptions[kind] = make(MessageHandlerSet)
	}
	ms.subscriptions[kind].Add(do)
	for sub := range ms.subscribers {
		sub.MessageAgent().Subscribe(kind, do)
	}
}

func (ms *messageSubscriber) Unsubscribe(kind string, do MessageHandler) {
	if handlers, ok := ms.subscriptions[kind]; ok {
		handlers.Remove(do)
	}
	for sub := range ms.subscribers {
		sub.MessageAgent().Unsubscribe(kind, do)
	}
}

func (ms *messageSubscriber) hasMember(d MessageDelegate) bool {
	_, ok := ms.subscribers[d]
	return ok
}

type eventSubscriber struct {
	subscribers   map[EventDelegate]struct{}
	subscriptions map[string]EventHandlerSet
}

func (es *eventSubscriber) Add(d EventDelegate) {
	if _, ok := es.subscribers[d]; ok {
		warn(ErrDuplicateDelegate)
		return
	}
	agent := d.EventAgent()
	for kind, handlers := range es.subscriptions {
		for _, h := range handlers {
			agent.Subscribe(kind, h)
		}
	}
	es.subscribers[d] = struct{}{}
}

func (es *eventSubscriber) Remove(d EventDelegate) {
	if _, ok := es.subscribers[d]; !ok {
		warn(ErrNoDelegates)
		return
	}
	agent := d.EventAgent()
	for kind, handlers := range es.subscriptions {
		for _, h := range handlers {
			agent.Unsubscribe(kind, h)
		}
	}
	delete(es.subscribers, d)
}

func (es *eventSubscriber) Subscribe(kind string, do EventHandler) {
	if _, ok := es.subscriptions[kind]; !ok {
		es.subscriptions[kind] = make(EventHandlerSet)
	}
	es.subscriptions[kind].Add(do)
	for sub := range es.subscribers {
		sub.EventAgent().Subscribe(kind, do)
	}
}

func (es *eventSubscriber) Unsubscribe(kind string, do EventHandler) {
	if handlers, ok := es.subscriptions[kind]; ok {
		handlers.Remove(do)
	}
	for sub := range es.subscribers {
		sub.EventAgent().Unsubscribe(kind, do)
	}
}

func (es *eventSubscriber) hasMember(d EventDelegate) bool {
	_, ok := es.subscribers[d]
	return ok
}
