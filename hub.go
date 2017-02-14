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

// Hub is an isolated system for communication among member EventResponders
// An EventResponder should only belong to a single Hub at any given time.
// Hub does not interact with messages at all.
type Hub struct {
	ID string

	families      map[string]*Family
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
			make(map[string]*Family),
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
	c.Messages.Delegate = c
	c.Events.Delegate = c

	return
}

func (h *Hub) NewFamily(id string) *Family {
	if _, ok := h.families[id]; ok {
		return h.families[id]
	}
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
	h.families[id] = f

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
