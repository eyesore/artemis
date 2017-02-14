package artemis

import "net/http"

type Client struct {
	ID string

	Messages *MessageAgent
	Events   *EventAgent
}

func NewClient(w http.ResponseWriter, r *http.Request) (*Client, error) {
	return DefaultHub().NewClient(w, r)
}

func (c *Client) EventAgent() *EventAgent {
	return c.Events
}

func (c *Client) MessageAgent() *MessageAgent {
	return c.Messages
}

func (c *Client) Trigger(eventKind string, data DataGetter) {
	c.Events.Hub.Broadcast(eventKind, data, c)
}

func (c *Client) PushMessage(m []byte, mtype int) {
	c.Messages.PushMessage(m, mtype)
}

func (c *Client) Join(families ...*Family) {
	for _, f := range families {
		f.Add(c)
	}
}

func (c *Client) Leave(f *Family) {
	f.Remove(c)
}

func (c *Client) BelongsTo(f *Family) bool {
	return f.hasMember(c)
}
