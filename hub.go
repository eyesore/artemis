package artemis

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	hubs = make(map[string]*Hub)

	// DefaultHub is a singleton that allows the library to be used without really worrying about
	// the Hub API.  If only a single hub is needed, then this is a fine solution.
	// the uuid suffix is static and completely arbitrary
	defaultHub   *Hub
	defaultHubID = "artemis:DefaultHub:74157a9f-9fd2-4030-b704-f3ce20eb6df7"

	// ErrDuplicateHubID indicates that hub creation failed because the name is already in use.
	ErrDuplicateHubID = errors.New("A hub with that ID already exists.")
)

// type DefaultEventResponder struct {
// 	H                  *Hub
// 	responder          interface{}
// 	events             chan Event
// 	readyForEvents     bool
// 	eventSubscriptions map[string]EventResponseSet
// }

// // JoinHub implements Event Responder
// func (d *DefaultEventResponder) JoinHub(h *Hub) {
// 	d.H = h
// }

// // OnEvent implements EventResponder
// func (d *DefaultEventResponder) OnEvent(kind string, do EventResponse) {
// 	if !d.readyForEvents {
// 		// lazy launch goroutine for event listening
// 		go d.startListening()
// 	}
// 	if _, ok := d.eventSubscriptions[kind]; !ok {
// 		d.eventSubscriptions[kind] = make(EventResponseSet)
// 	}
// 	var responder interface{}
// 	if d.responder != nil {
// 		responder = d.responder
// 	} else {
// 		responder = d
// 	}

// 	d.eventSubscriptions[kind].Add(do)
// 	d.H.Subscribe(kind, d.events, responder)
// }

// // OffEvent implements EventResponder
// func (d *DefaultEventResponder) OffEvent(kind string, do EventResponse) {
// 	if actions, ok := d.eventSubscriptions[kind]; ok {
// 		actions.Remove(do)
// 	}
// 	d.H.Unsubscribe(kind, d.events)
// }

// func (d *DefaultEventResponder) noEventSubscriptions() bool {
// 	for _, responses := range d.eventSubscriptions {
// 		if len(responses) > 0 {
// 			return false
// 		}
// 	}
// 	return true
// }

// func (d *DefaultEventResponder) startListening() {
// 	defer close(d.events)
// 	d.readyForEvents = true
// 	for {
// 		ev, ok := <-d.events
// 		if !ok {
// 			break
// 		}
// 		if actions, ok := d.eventSubscriptions[ev.Kind]; ok {
// 			for _, do := range actions {
// 				do(d, ev.Data)
// 			}
// 		}
// 	}

// 	warn(ErrEventChannelHasClosed)
// }

// Hub is an isolated system for communication among member EventResponders
// An EventResponder should only belong to a single Hub at any given time.
// Hub does not interact with messages at all.
type Hub struct {
	ID            string
	subscriptions map[string]SubscriptionSet
}

// NewHub creates a new Hub with a unique name. If the ID is already in use
// NewHub returns the hub with that ID as well as ErrDuplicateHubID
func NewHub(id string) (*Hub, error) {
	if _, ok := hubs[id]; ok {
		// TODO testcase for ErrDuplicate with h returned
		return hubs[id], ErrDuplicateHubID
	}

	h := &Hub{}
	h.ID = id
	h.subscriptions = make(map[string]SubscriptionSet)
	hubs[id] = h

	return h, nil
}

// DefaultHub can be used in situations where all EventResponders in the app
// share the same namespace and are allowed to communicate with one another.
// It is loaded lazily the first time this function is called.
func DefaultHub() *Hub {
	if defaultHub == nil {
		defaultHub = &Hub{
			defaultHubID,
			make(map[string]SubscriptionSet),
		}
	}

	return defaultHub
}

func (h *Hub) NewClient(w http.ResponseWriter, r *http.Request) (c *Client, err error) {
	c = &Client{}

	c.Messages, err = h.NewMessageAgent(w, r)
	if err != nil {
		return nil, err
	}
	c.Events = h.NewEventAgent()

	return
}

func (h *Hub) NewFamily() *Family {
	f := &Family{}
	f.Hub = h

	f.Messages = messageSubscriber{
		make(map[MessageDelegate]struct{}),
		make(map[string]MessageHandlerSet),
	}
	f.Events = eventSubscriber{
		make(map[EventDelegate]struct{}),
		make(map[string]EventHandlerSet),
	}

	return f
}

func (h *Hub) NewEventAgent() *EventAgent {
	a := &EventAgent{}
	a.Hub = h
	a.events = make(chan *Event, 256)
	a.ready = false
	a.subscriptions = make(map[string]EventHandlerSet)

	return a
}

// TODO tj - this should be protocol agnostic - for now, just pass in the http params
func (h *Hub) NewMessageAgent(w http.ResponseWriter, r *http.Request) (*MessageAgent, error) {
	agent := &MessageAgent{}
	err := agent.connect(w, r)
	if err != nil {
		return nil, err
	}
	agent.Hub = h

	agent.sendText = make(chan []byte, 256)
	agent.sendBinary = make(chan []byte, 256)
	agent.subscriptions = make(map[string]MessageHandlerSet)

	return agent, nil
}

// PushMessage implements MessagePusher
// func (h *Hub) PushMessage(m []byte, messageType int) {

// }

// Broadcast informs all subscribed listeners to eventKind of the event.  Source is optionally
// available as source of the event, and can be nil.
func (h *Hub) Broadcast(eventKind string, data DataGetter, source interface{}) {
	if subscribers, ok := h.subscriptions[eventKind]; ok {
		for sub := range subscribers {
			e := newEvent(eventKind, data)
			e.Source = source
			sub <- e
		}
	} else {
		warn(fmt.Errorf("Hub fired event of kind '%s' but no one was listening.", eventKind))
	}
}

// Subscribe sets up a subscriptions to a named event, events will be sent over the channel
func (h *Hub) subscribe(kind string, c chan *Event) {
	if _, ok := h.subscriptions[kind]; !ok {
		h.subscriptions[kind] = make(SubscriptionSet)
	}
	// silent on duplicate
	h.subscriptions[kind].Add(c)
}

func (h *Hub) unsubscribe(kind string, c chan *Event) {
	if _, ok := h.subscriptions[kind]; ok {
		h.subscriptions[kind].Remove(c)
	}
}

// TODO hub graceful destruction
