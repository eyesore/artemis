// Package artemis manages Socket connections and provides an API for handling incoming messages,
// sending messages back to the client, and subscribing to and firing server-side events.
//
// artemis is a wordification of rtmes, which stands for Real Time Message & Event Server
package artemis

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// TODO some of these can go in artemis.go
var (
	// DefaultTextParser can be overridden to implement text parsing for Client without
	// providing a custom parser
	DefaultTextParser = ParseJSONMessage

	// Timeout is the time allowed to write messages
	Timeout     = 10 * time.Second
	pongTimeout = Timeout * 6
	pingPeriod  = (pongTimeout * 9) / 10

	// Default WS configs - can be set at package level
	// TODO, update for multiple protocols
	ReadLimit                       int64 = 4096
	HandshakeTimeout                      = 10 * time.Second
	ReadBufferSize, WriteBufferSize int

	// Errors sends errors encountered during send and receive and is meant to be consumed by a logger
	// TODO provide default logger to stdout
	Errors = make(chan error)

	// ErrHubMismatch occurs when trying to add a client to family with a different hub.
	ErrHubMismatch = errors.New("Unable to add a client to a family in a different hub.")

	// ErrDuplicateClient occurs when a client ID matches an existing client ID in the family on join.
	ErrDuplicateClient = errors.New("Tried to add a duplicate client to family.")

	// ErrAlreadySubscribed occurs when trying to add an event handler to a Responder that already has one.
	ErrAlreadySubscribed = errors.New("Trying to add duplicate event to responder.")

	// ErrUnparseableMessage indicates that a message does not contain some expected data.
	ErrUnparseableMessage = errors.New("The message parser does not recognize the message format.")

	// ErrDuplicateAction means that a MessageResponder is already listening to perform the same action
	// in response to the same message
	ErrDuplicateAction = errors.New("Unable to add that action again for the same message name.")

	ErrIllegalPingTimeout = errors.New("pingPeriod must be shorter than pongTimeout")

	errNotYetImplemented = errors.New("You are trying to use a feature that has not been implemented yet.")
)

// TODO support multiple socket protocols

// SetPingPeriod allows the application to specify the period between sending ping messages to clients
func SetPingPeriod(n time.Duration) error {
	if n >= pongTimeout {
		return ErrIllegalPingTimeout
	}
	pingPeriod = n
	return nil
}

// SetPongTimeout allows the application to specify the period allowed to receive a pong message from clients
func SetPongTimeout(n time.Duration) error {
	if n <= pingPeriod {
		return ErrIllegalPingTimeout
	}
	pongTimeout = n
	return nil
}

// ParseJSONMessage parses a Message from a byte stream if possible.
func ParseJSONMessage(m []byte) (*Message, error) {
	var messageName interface{}
	var ok bool
	pm := make(map[string]interface{})
	err := json.Unmarshal(m, pm)
	if err != nil {
		return nil, err
	}
	if messageName, ok = pm["name"]; !ok {
		return nil, ErrUnparseableMessage
	}
	mdg := &Message{messageName.(string), pm, m}

	return mdg, err
}

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

	messageSubscriptions map[string]ActionSet
	eventSubscriptions   map[string]ActionSet
	conn                 *websocket.Conn
	sendText             chan []byte
	sendBinary           chan []byte
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

	c.messageSubscriptions = make(map[string]ActionSet)
	c.eventSubscriptions = make(map[string]ActionSet)

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
func (c *Client) OnMessage(kind string, do Action) {
	if _, ok := c.messageSubscriptions[kind]; !ok {
		c.messageSubscriptions[kind] = make(ActionSet)
	}
	c.messageSubscriptions[kind].Add(do)
}

// OffMessage implements MessageListener
func (c *Client) OffMessage(kind string, do Action) {
	if actions, ok := c.messageSubscriptions[kind]; ok {
		actions.Remove(do)
	}
}

// StopListening implements MessageListener
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

// Client implements MessageResponder
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
func (c *Client) OnEvent(kind string, do Action) {
	// Single channel will receive all events from hub
	// Hub stores channel in map[string]chan
	// key is client id
	// sends all events on chan
	// when client unregisters from event, check to see if any event subscriptions remain.
	// if not, close channel
}

// OffEvent implements EventResponder
func (c *Client) OffEvent(kind string) {

}

// PushMessage implements MessagePusher
func (c *Client) PushMessage(m []byte, messageType int) {

}

// Trigger informs the hub containing this instance of Client to notify all subscribers to event.
func (c *Client) Trigger(event string, data interface{}) {

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
	defer c.cleanup()

	c.conn.SetReadLimit(ReadLimit)
	c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	c.conn.SetPongHandler(c.handlePong)
	c.conn.SetCloseHandler(c.handleClose)

	for {
		mtype, m, err := c.conn.ReadMessage()
		if err != nil {
			Errors <- err
			c.cleanup()
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
		c.cleanup()
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

func (c *Client) cleanup() {
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
	c.cleanup()
	return nil
}
