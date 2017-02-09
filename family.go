package artemis

import "fmt"

// Family is a category for clients.  It provides a convenient way to subscribe a group
// of clients that should function similarly to the same events and messages.
// Importantly, the Family only passes events directly to member Clients to handle themselves.
type Family struct {
	H *Hub

	// TODO - keys can probably be client refs... no need for ID?
	Clients              map[string]*Client
	messageSubscriptions map[string]MessageResponseSet
	eventSubscriptions   map[string]EventResponseSet
}

// NewFamily creates a new instance of Family and adds it to a hub.
func NewFamily(h *Hub) *Family {
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
	if _, ok := f.eventSubscriptions[kind]; !ok {
		f.eventSubscriptions[kind] = make(EventResponseSet)
	}
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
	if _, ok := f.Clients[c.ID]; ok {
		warn(ErrDuplicateClient)
		return
	}

	for kind, actions := range f.messageSubscriptions {
		for _, do := range actions {
			c.OnMessage(kind, do)
		}
	}
	for kind, actions := range f.eventSubscriptions {
		for _, do := range actions {
			c.OnEvent(kind, do)
		}
	}
	f.Clients[c.ID] = c
}

func (f *Family) remove(c *Client) {
	if _, ok := f.Clients[c.ID]; !ok {
		warn(fmt.Errorf("Trying to remove non-member client from family."))
		return
	}

	for kind, actions := range f.eventSubscriptions {
		for _, do := range actions {
			c.OffEvent(kind, do)
		}
	}
	for kind, actions := range f.messageSubscriptions {
		for _, do := range actions {
			c.OffMessage(kind, do)
		}
	}
	delete(f.Clients, c.ID)
}

func (f *Family) hasMember(c *Client) bool {
	_, ok := f.Clients[c.ID]
	return ok
}
