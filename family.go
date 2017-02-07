package artemis

import "net/http"

// Family is a category for clients.  It provides a convenient way to subscribe a group
// of clients that should function similarly to the same events and messages.
// Importantly, the Family only passes events directly to member Clients to handle themselves.
type Family struct {
	H                    *Hub
	Clients              map[string]*Client
	messageSubscriptions map[string]ActionSet
	eventSubscriptions   map[string]ActionSet
}

// NewFamily creates a new instance of Family and adds it to a hub.
func NewFamily(r *http.Request, w http.ResponseWriter, h *Hub) *Family {
	if h == nil {
		h = DefaultHub()
	}

	f := &Family{}
	f.Clients = make(map[string]*Client)
	f.messageSubscriptions = make(map[string]ActionSet)
	f.eventSubscriptions = make(map[string]ActionSet)
	h.Add(f)

	return f
}

// JoinHub implements EventResponder
func (f *Family) JoinHub(h *Hub) {
	f.H = h
}

// OnMessage implements MessageListener
func (f *Family) OnMessage(kind string, do Action) {
	f.messageSubscriptions[kind].Add(do)
	for _, c := range f.Clients {
		c.OnMessage(kind, do)
	}
}

// OffMessage implements MessageListener
func (f *Family) OffMessage(kind string) {
	delete(f.messageSubscriptions, kind)
	for _, c := range f.Clients {
		c.OffMessage(kind)
	}
}

// OnEvent implements EventResponder
func (f *Family) OnEvent(kind string, do Action) {
	f.OffEvent(kind)
	f.eventSubscriptions[kind].Add(do)
	for _, c := range f.Clients {
		c.OnEvent(kind, do)
	}
}

// OffEvent implements EventResponder
func (f *Family) OffEvent(kind string) {
	delete(f.eventSubscriptions, kind)
	for _, c := range f.Clients {
		c.OffEvent(kind)
	}
}

// PushMessage implements MessagePusher
func (f *Family) PushMessage(m []byte) {

}

func (f *Family) add(c *Client) {
	// don't do anything if the client already exists here
	// TODO - this being silent is probably not OK - log to info
	if _, ok := f.Clients[c.ID]; ok {
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
