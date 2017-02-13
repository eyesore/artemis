package artemis

import (
	"errors"
	"fmt"
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
type EventResponse func(EventResponder, DataGetter)

// EventResponseSet stores a set of unique actions.  Comparison is based on string value of fn.
type EventResponseSet map[string]EventResponse

// EventResponderSet is predicated on being able to distinguish between functions to prevent
// duplicate adds and to allow removal.  This proves difficult to do.
// The go language spec states that functions are not comparable -
// therefore, there is no guarantee that this technique will work in the future, or at all
// Link: http://stackoverflow.com/a/42147285/1375316
// TODO compare interfaces instead of fns?
func getEventResponseKey(r EventResponse) string {
	return fmt.Sprintf("%v", r)
}

// Add puts a new EventResponse into the set.  Warns asynchronously if r is already in the set.
func (ers EventResponseSet) Add(r EventResponse) {
	key := getEventResponseKey(r)
	if _, ok := ers[key]; ok {
		warn(ErrDuplicateAction)
		return
	}
	ers[key] = r
}

// Remove ensures that EventResponse "a" is no longer present in the EventResponseSet
func (ers EventResponseSet) Remove(r EventResponse) {
	key := getEventResponseKey(r)
	// if key is not there, doesn't matter
	delete(ers, key)
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

type DefaultEventResponder struct {
	ID                 string
	H                  *Hub
	events             chan Event
	readyForEvents     bool
	eventSubscriptions map[string]EventResponseSet
}

// JoinHub implements Event Responder
func (d *DefaultEventResponder) JoinHub(h *Hub) {
	d.H = h
}

// OnEvent implements EventResponder
func (d *DefaultEventResponder) OnEvent(kind string, do EventResponse) {
	if !d.readyForEvents {
		// lazy launch goroutine for event listening
		go d.startListening()
	}
	if _, ok := d.eventSubscriptions[kind]; !ok {
		d.eventSubscriptions[kind] = make(EventResponseSet)
	}

	d.eventSubscriptions[kind].Add(do)
	d.H.Subscribe(kind, d.events)
}

// OffEvent implements EventResponder
func (d *DefaultEventResponder) OffEvent(kind string, do EventResponse) {
	if actions, ok := d.eventSubscriptions[kind]; ok {
		actions.Remove(do)
	}
	d.H.Unsubscribe(kind, d.events)
}

func (d *DefaultEventResponder) noEventSubscriptions() bool {
	for _, responses := range d.eventSubscriptions {
		if len(responses) > 0 {
			return false
		}
	}
	return true
}

func (d *DefaultEventResponder) startListening() {
	defer close(d.events)
	d.readyForEvents = true
	for {
		ev, ok := <-d.events
		if !ok {
			break
		}
		if actions, ok := d.eventSubscriptions[ev.Kind]; ok {
			for _, do := range actions {
				do(d, ev.Data)
			}
		}
	}

	warn(ErrEventChannelHasClosed)
}

// Hub is an isolated system for communication among member EventResponders
// An EventResponder should only belong to a single Hub at any given time.
// Hub does not interact with messages at all.
type Hub struct {
	ID            string
	members       []EventResponder // used for push? IDK they don't have to be EventResponders
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
	h.members = make([]EventResponder, 0)
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
		warn(fmt.Errorf("Hub fired event of kind '%s' but no one was listening.", eventKind))
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
