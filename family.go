package artemis

import "net/http"

// Family is a category for clients.  It provides a convenient way to subscribe a group
// of clients that should function similarly to the same events and messages.
// Importantly, the Family only passes events directly to member Clients to handle themselves.
type Family struct {
	H                    *Hub
	Clients              map[string]*Client
	messageSubscriptions map[string]MessageResponseSet
	eventSubscriptions   map[string]EventResponseSet
}

// NewFamily creates a new instance of Family and adds it to a hub.
func NewFamily(r *http.Request, w http.ResponseWriter, h *Hub) *Family {
	if h == nil {
		h = DefaultHub()
	}

	f := &Family{}
	f.Clients = make(map[string]*Client)
	f.messageSubscriptions = make(map[string]MessageResponseSet)
	f.eventSubscriptions = make(map[string]EventResponseSet)
	h.Add(f)

	return f
}

// JoinHub implements EventResponder
func (f *Family) JoinHub(h *Hub) {
	f.H = h
}

// OnMessage implements MessageListener
func (f *Family) OnMessage(kind string, do MessageResponse) {
	f.messageSubscriptions[kind].Add(do)
	for _, c := range f.Clients {
		c.OnMessage(kind, do)
	}
}

// OffMessage implements MessageListener
func (f *Family) OffMessage(kind string, do MessageResponse) {
	if actions, ok := f.messageSubscriptions[kind]; ok {
		actions.Remove(do)
	}
	for _, c := range f.Clients {
		c.OffMessage(kind, do)
	}
}

// OnEvent implements EventResponder
func (f *Family) OnEvent(kind string, do EventResponse) {
	f.eventSubscriptions[kind].Add(do)
	for _, c := range f.Clients {
		c.OnEvent(kind, do)
	}
}

// OffEvent implements EventResponder
func (f *Family) OffEvent(kind string, do EventResponse) {
	delete(f.eventSubscriptions, kind)
	for _, c := range f.Clients {
		c.OffEvent(kind, do)
	}
}

// PushMessage implements MessagePusher
func (f *Family) PushMessage(m []byte, messageType int) {
	for _, c := range f.Clients {
		c.PushMessage(m, messageType)
	}
}

func (f *Family) add(c *Client) {
	// don't do anything if the client already exists here
	// TODO separate channel for errors that aren't that important (sic)
	if _, ok := f.Clients[c.ID]; ok {
		Errors <- ErrDuplicateClient
		return
	}

	for kind, actions := range f.messageSubscriptions {
		for action := range actions {
			do := *action
			c.OnMessage(kind, do)
		}
	}
	f.Clients[c.ID] = c
}
