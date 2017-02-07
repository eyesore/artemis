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

var (
	// DefaultTextParser can be overridden to implement text parsing for Client without
	// providing a custom parser
	DefaultTextParser = ParseJSONMessage

	// Default WS configs - can be set at package level
	// TODO, update for multiple protocols
	ReadLimit                       int64 = 4096
	HandshakeTimeout                      = 10 * time.Second
	ReadBufferSize, WriteBufferSize int

	// ErrHubMismatch occurs when trying to add a client to family with a different hub.
	ErrHubMismatch = errors.New("Unable to add a client to a family in a different hub.")

	// ErrAlreadySubscribed occurs when trying to add an event handler to a Responder that already has one.
	ErrAlreadySubscribed = errors.New("Trying to add duplicate event to responder.")

	// ErrUnparseableMessage indicates that a message does not contain some expected data.
	ErrUnparseableMessage = errors.New("The message parser does not recognize the message format.")

	// ErrDuplicateAction means that a MessageResponder is already listening to perform the same action
	// in response to the same message
	ErrDuplicateAction = errors.New("Unable to add that action again for the same message name.")

	errNotYetImplemented = errors.New("You are trying to use a feature that has not been implemented yet.")
)

// TODO support multiple socket protocols

// MessageParser parses byte streams into MessageDataGetters
type MessageParser interface {
	ParseText([]byte) (MessageDataGetter, error)
	ParseBinary([]byte) (MessageDataGetter, error)
}

// MessageListener manages the susbscriptions of one or more MessageResponders
type MessageListener interface {
	OnMessage(string, Action)
	OffMessage(string)
}

// MessageResponder can respond to a received message by doing an Action
type MessageResponder interface {
	MessageListener
	// Client returns the client that received the message being responded to.
	// All message responders must have some way of keeping track of which client received the message,
	// in order to pass it to Action
	Client() *Client
	MessageRespond(MessageDataGetter)
}

// MessageParseResponder handles incoming messages, and is also capable of parsing them.
type MessageParseResponder interface {
	MessageParser
	MessageResponder
}

// MessagePusher can send a message over an existing WS connection
type MessagePusher interface {
	PushMessage([]byte)
}

// MessageDataGetter is a DataGetter that also exposes the raw message from the client, and has a Name
type MessageDataGetter interface {
	DataGetter
	Raw() []byte
	Name() string
}

// Message is a basic implementation of MessageDataGetter.
type Message struct {
	name string
	data interface{}
	raw  []byte
}

func (m *Message) Data() interface{} {
	return m.data
}

func (m *Message) Raw() []byte {
	return m.raw
}

func (m *Message) Name() string {
	return m.name
}

// Action is a function that is executed in response to an event or message.
type Action func(*Client, DataGetter)

// ActionSet stores a set of unique actions.  Comparison is based on function pointer identity.
type ActionSet map[*Action]struct{}

// Add adds a new Action to the action set.  Returns error if the Action is already in the set.
func (as ActionSet) Add(a Action) error {
	if _, ok := as[&a]; ok {
		return ErrDuplicateAction
	}
	as[&a] = struct{}{}
	return nil
}

// Remove ensures that Action "a" is no longer present in the ActionSet
func (as ActionSet) Remove(a Action) {
	// if key is not there, doesn't matter
	delete(as, &a)
}

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

	messageHandlers map[string]ActionSet
	eventHandlers   map[string]ActionSet
	conn            *websocket.Conn
	send            chan []byte
}

// NewClient creats a client, sets up incoming and outgoing message pipes, and
// registers with a Hub
func NewClient(r *http.Request, w http.ResponseWriter, h *Hub) (*Client, error) {
	if h == nil {
		h = DefaultHub()
	}

	c := &Client{}
	c.send = make(chan []byte, 256)
	// TODO accept non-http connections
	err := c.connect(r, w)
	if err != nil {
		return nil, err
	}
	h.Add(c)
	c.messageHandlers = make(map[string]ActionSet)
	c.eventHandlers = make(map[string]ActionSet)

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

// OnMessage implements MessageResponder
func (c *Client) OnMessage(kind string, do Action) {
	if _, ok := c.messageHandlers[kind]; !ok {
		c.messageHandlers[kind] = make(ActionSet)
	}
	c.messageHandlers[kind].Add(do)
}

// OffMessage implements MessageResponder
func (c *Client) OffMessage(kind string, do Action) {
	if actions, ok := c.messageHandlers[kind]; ok {
		actions.Remove(do)
	}
}

func (c *Client) StopListening(kind string) {
	delete(c.messageHandlers, kind)
}

// MessageRespond implments MessageResponder
func (c *Client) MessageRespond(mdg MessageDataGetter) {
	if c.MR != nil {
		c.MR.MessageRespond(mdg)
	}

	messageName := mdg.Name()
	if actions, ok := c.messageHandlers[messageName]; ok {
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
func (c *Client) PushMessage(m []byte) {

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
	defer func() {
		c.conn.Close()
	}()

	c.conn.SetReadLimit(ReadLimit)
	// TODO tj - set read and write deadline, and close connection if disconnect
	// is detected

	for {
		mtype, m, err := c.conn.ReadMessage()
		if err != nil {
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
			// do what?
		}
		c.MessageRespond(message)
	case websocket.TextMessage:
		message, err := c.ParseText(m)
		if err != nil {
			// send err on channel?
		}
		c.MessageRespond(message)
	default:
		// do some error thing?
	}
}

func (c *Client) startWriting() {

}
