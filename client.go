package artemis

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Client is a representation of a connected 3rd party (client).  This should be the entry point of
// all messages.
// It maintainss a socket connection until the Client is destroyed.
// Client is an EventResponder, MessageParseResponder, and MessagePusher
type Client struct {
	ID string
	H  *Hub

	// MP defaults to nil, and if not set Clint will attempt to parse all incoming messages.
	// If a parser is set, Client will hand off all MessageParsing responsibilites to MP.
	MP MessageParser

	// MR defaults to nil, but can be set to allow a custom MessageReponder to respond to
	// messages received by Client.  Client will still also respond (after MR) if configured
	// to do so.
	MR MessageResponder

	messageSubscriptions map[string]MessageResponseSet
	eventSubscriptions   map[string]EventResponseSet
	conn                 *websocket.Conn
	sendText             chan []byte
	sendBinary           chan []byte
	events               chan Event
	readyForEvents       bool
}

// NewClient creats a client, sets up incoming and outgoing message pipes, and
// registers with a Hub
func NewClient(r *http.Request, w http.ResponseWriter, h *Hub) (*Client, error) {
	if h == nil {
		h = DefaultHub()
	}

	c := &Client{}
	// TODO accept non-http connections
	err := c.connect(r, w)
	if err != nil {
		return nil, err
	}
	h.Add(c)

	// send messages on channel, to ensure all writes go out on the same goroutine
	c.sendText = make(chan []byte, 256)
	c.sendBinary = make(chan []byte, 256)
	c.events = make(chan Event, 256)
	c.readyForEvents = false

	c.messageSubscriptions = make(map[string]MessageResponseSet)
	c.eventSubscriptions = make(map[string]EventResponseSet)

	return c, err
}

// Join  allows the client to join many families at once.
// A client cannot join a family in a different hub,
// and this method will return an error if that is attempted, but will still join
// any valid Families passed.
// It is safe to join the same family more than once, and this has no effect.
func (c *Client) Join(families ...*Family) (err error) {
	for _, f := range families {
		if f.H != c.H {
			err = ErrHubMismatch
			continue
		}
		f.add(c)
	}
	return err
}

// OnMessage implements MessageListener
func (c *Client) OnMessage(kind string, do MessageResponse) {
	if _, ok := c.messageSubscriptions[kind]; !ok {
		c.messageSubscriptions[kind] = make(MessageResponseSet)
	}
	c.messageSubscriptions[kind].Add(do)
}

// OffMessage implements MessageListener
func (c *Client) OffMessage(kind string, do MessageResponse) {
	if actions, ok := c.messageSubscriptions[kind]; ok {
		actions.Remove(do)
	}
}

// StopListening implements MessageListener
// TODO tj - make same for events and differentiate
func (c *Client) StopListening(kind string) {
	delete(c.messageSubscriptions, kind)
}

// MessageRespond implments MessageResponder
func (c *Client) MessageRespond(mdg MessageDataGetter) {
	if c.MR != nil {
		c.MR.MessageRespond(mdg)
	}

	messageName := mdg.Name()
	if actions, ok := c.messageSubscriptions[messageName]; ok {
		for action := range actions {
			do := *action
			do(c.Client(), mdg)
		}
	}
	// TODO tj else log skip? debug only?
}

// Client implements MessageResponder - TODO - get rid of
func (c *Client) Client() *Client {
	return c
}

// ParseText implements MessageParser
func (c *Client) ParseText(m []byte) (MessageDataGetter, error) {
	if c.MP != nil {
		return c.MP.ParseText(m)
	}
	return DefaultTextParser(m)
}

// ParseBinary implements MessageParser
func (c *Client) ParseBinary(m []byte) (MessageDataGetter, error) {
	if c.MP != nil {
		return c.MP.ParseBinary(m)
	}
	// TODO
	return nil, errNotYetImplemented
}

// JoinHub implements Event Responder
func (c *Client) JoinHub(h *Hub) {
	c.H = h
}

// OnEvent implements EventResponder
func (c *Client) OnEvent(kind string, do EventResponse) {
	if !c.readyForEvents {
		// lazy launch goroutine for event listening
		go c.startListening()
		// TODO mutex needed?
		c.readyForEvents = true
	}
	if _, ok := c.eventSubscriptions[kind]; !ok {
		c.eventSubscriptions[kind] = make(EventResponseSet)
	}
	c.eventSubscriptions[kind].Add(do)
	c.H.Subscribe(kind, c.events)
}

// OffEvent implements EventResponder
func (c *Client) OffEvent(kind string, do EventResponse) {
	if actions, ok := c.eventSubscriptions[kind]; ok {
		actions.Remove(do)
	}
	c.H.Unsubscribe(kind, c.events)
	if c.noEventSubscriptions() {
		c.eventCleanup()
	}
}

func (c *Client) noEventSubscriptions() bool {
	for _, responses := range c.eventSubscriptions {
		if len(responses) > 0 {
			return false
		}
	}
	return true
}

// PushMessage implements MessagePusher
func (c *Client) PushMessage(m []byte, messageType int) {
	switch messageType {
	case websocket.BinaryMessage:
		c.sendBinary <- m
	case websocket.TextMessage:
		c.sendText <- m
	default:
		// TODO tj probably not the right error
		Errors <- ErrUnparseableMessage
	}
}

// Trigger informs the hub containing this instance of Client to notify all subscribers to event.
func (c *Client) Trigger(eventKind string, data DataGetter) {
	c.H.Fire(eventKind, data)
}

func (c *Client) connect(r *http.Request, w http.ResponseWriter) error {
	upgrader := websocket.Upgrader{
		HandshakeTimeout: HandshakeTimeout,
		ReadBufferSize:   ReadBufferSize,
		WriteBufferSize:  WriteBufferSize,
	}
	// TODO add response header
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	c.conn = conn
	go c.startReading()
	go c.startWriting()

	return nil
}

func (c *Client) startReading() {
	defer c.messageCleanup()

	c.conn.SetReadLimit(ReadLimit)
	c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	c.conn.SetPongHandler(c.handlePong)
	c.conn.SetCloseHandler(c.handleClose)

	for {
		mtype, m, err := c.conn.ReadMessage()
		if err != nil {
			Errors <- err
			c.messageCleanup()
			break
		}
		c.receiveMessage(mtype, m)
	}
}

func (c *Client) receiveMessage(mtype int, m []byte) {
	switch mtype {
	case websocket.BinaryMessage:
		message, err := c.ParseBinary(m)
		if err != nil {
			Errors <- err
		}
		c.MessageRespond(message)
	case websocket.TextMessage:
		message, err := c.ParseText(m)
		if err != nil {
			Errors <- err
		}
		c.MessageRespond(message)
	default:
		Errors <- ErrUnparseableMessage
	}
}

func (c *Client) startWriting() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.messageCleanup()
	}()

	for {
		select {
		case message, ok := <-c.sendText:
			if !ok {
				// TODO tj - this probably won't happen right now, but if it does, clean up
				return
			}
			c.doWrite(websocket.TextMessage, message)
		case message, ok := <-c.sendBinary:
			if !ok {
				// same as above
				return
			}
			c.doWrite(websocket.BinaryMessage, message)
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(Timeout)); err != nil {
				// cleanup
				return
			}
		}
	}
}

func (c *Client) doWrite(messageType int, m []byte) {
	c.conn.SetWriteDeadline(time.Now().Add(Timeout))
	if err := c.conn.WriteMessage(messageType, m); err != nil {
		Errors <- err
	}
}

func (c *Client) messageCleanup() {
	if _, ok := <-c.sendBinary; ok {
		close(c.sendBinary)
	}
	if _, ok := <-c.sendText; ok {
		close(c.sendText)
	}
	c.conn.WriteControl(websocket.CloseNormalClosure, []byte{}, time.Now().Add(Timeout))
	c.conn.Close()
}

func (c *Client) handlePong(pong string) error {
	c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	return nil
}

func (c *Client) handleClose(code int, text string) error {
	c.messageCleanup()
	return nil
}

func (c *Client) startListening() {
	defer c.eventCleanup()

	for ev, ok := <-c.events; ok; {
		if actions, ok := c.eventSubscriptions[ev.Kind]; ok {
			for action := range actions {
				do := *action
				do(ev.Data)
			}
		}
	}

	Errors <- ErrEventChannelHasClosed
}

func (c *Client) eventCleanup() {
	if _, ok := <-c.events; ok {
		c.readyForEvents = false
		close(c.events)
	}
}
