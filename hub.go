package artemis

import "errors"

var (
	hubs = make(map[string]*Hub)

	// DefaultHub is a singleton that allows the library to be used without really worrying about
	// the Hub API.  If only a single hub is needed, then this is a fine solution.
	defaultHub   *Hub
	defaultHubID = "artemis:DefaultHub"

	// ErrDuplicateHubID indicates that hub creation failed because the name is already in use.
	ErrDuplicateHubID = errors.New("A hub with that ID already exists.")
)

// EventResponder can respond to an event by doing an MessageResponse
type EventResponder interface {
	JoinHub(*Hub)
	OnEvent(string, EventResponse)
	OffEvent(string, EventResponse)
}

// Event contains information about a hub-wide event.
type Event struct {
	Kind string
	Data DataGetter
}

// DataGetter gets data.
type DataGetter interface {
	Data() interface{}
}

// EventData is a basic implementation of DataGetter
type EventData struct {
	data interface{}
}

// Data implements DataGetter
func (ed *EventData) Data() interface{} {
	return ed.data
}

// TODO differentiate from MessageResponse or consolidate

// EventResponse is a function that is executed in response to a message.
type EventResponse func(DataGetter)

// EventResponseSet stores a set of unique actions.  Comparison is based on function pointer identity.
type EventResponseSet map[*EventResponse]struct{}

// Add adds a new EventResponse to the action set.  Returns error if the EventResponse is already in the set.
func (ers EventResponseSet) Add(r EventResponse) error {
	if _, ok := ers[&r]; ok {
		return ErrDuplicateAction
	}
	ers[&r] = struct{}{}
	return nil
}

// Remove ensures that EventResponse "a" is no longer present in the EventResponseSet
func (ers EventResponseSet) Remove(r EventResponse) {
	// if key is not there, doesn't matter
	delete(ers, &r)
}

type SubscriptionSet map[chan Event]struct{}

func (ss SubscriptionSet) Add(c chan Event) {
	if _, ok := ss[c]; !ok {
		ss[c] = struct{}{}
	}
}

func (ss SubscriptionSet) Remove(c chan Event) {
	delete(ss, c)
}

// Hub is an isolated system for communication among member EventResponders
// An EventResponder should only belong to a single Hub at any given time.
// Hub does not interact with messages at all.
type Hub struct {
	ID            string
	members       []EventResponder // used for push? IDK they don't have to be EventResponders
	subscriptions map[string]SubscriptionSet
}

// NewHub creates a new Hub with a unique name.
func NewHub(id string) (*Hub, error) {
	if _, ok := hubs[id]; ok {
		return nil, ErrDuplicateHubID
	}

	h := &Hub{}
	h.ID = id
	h.members = make([]EventResponder, 0)
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
			make([]EventResponder, 0),
			make(map[string]SubscriptionSet),
		}
	}

	return defaultHub
}

// Add places a Responder into the hub.  All events that the hub receives will be
func (h *Hub) Add(r EventResponder) {
	r.JoinHub(h)
}

// PushMessage implements MessagePusher
func (h *Hub) PushMessage(m []byte, messageType int) {

}

// Fire tells a hub to inform all subscribers of the event
func (h *Hub) Fire(eventKind string, data DataGetter) {
	if subscribers, ok := h.subscriptions[eventKind]; ok {
		for sub := range subscribers {
			sub <- Event{eventKind, data}
		}
	} else {
		Errors <- ErrNoSubscribers
	}
}

// Subscribe sets up a subscriptions to a named event, events will be sent over the channel
func (h *Hub) Subscribe(kind string, c chan Event) {
	if _, ok := h.subscriptions[kind]; !ok {
		h.subscriptions[kind] = make(SubscriptionSet)
	}
	// silent on duplicate
	h.subscriptions[kind].Add(c)
}

func (h *Hub) Unsubscribe(kind string, c chan Event) {
	if _, ok := h.subscriptions[kind]; ok {
		h.subscriptions[kind].Remove(c)
	}
}

// TODO remove responder from hub
// TODO hub graceful destruction
