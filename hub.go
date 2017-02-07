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

// EventResponder can respond to an event by doing an Action
type EventResponder interface {
	JoinHub(*Hub)
	OnEvent(string, Action)
	OffEvent(string)
}

type DataGetter interface {
	Data() interface{}
}

// EventData is a basic implementation of DataGetter
type EventData struct {
	data interface{}
}

func (ed *EventData) Data() interface{} {
	return ed.data
}

// Hub is an isolated system for communication among member EventResponders
// An EventResponder should only belong to a single Hub at any given time.
// Hub does not interact with messages at all.
type Hub struct {
	ID      string
	members []EventResponder
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
		}
	}

	return defaultHub
}

// Add places a Responder into the hub.  All events that the hub receives will be
func (h *Hub) Add(r EventResponder) {
	r.JoinHub(h)
}

// TODO remove responder from hub
// TODO hub graceful destruction
