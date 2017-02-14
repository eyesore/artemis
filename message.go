package artemis

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// MessageParser parses bytes into ParsedMessages
type MessageParser interface {
	ParseText([]byte) (*ParsedMessage, error)
	ParseBinary([]byte) (*ParsedMessage, error)
}

type ParsedMessage struct {
	Value interface{}
	Raw   []byte
	Kind  string
}

func NewParsedMessage(kind string, data interface{}, raw []byte) *ParsedMessage {
	pm := &ParsedMessage{}
	pm.Kind = kind
	pm.Value = data
	pm.Raw = raw

	return pm
}

type Message struct {
	Kind      string
	Data      interface{}
	Recipient interface{}
	Source    *MessageAgent

	Raw []byte
}

// MessageResponse is a function that is executed in response to a message.
type MessageHandler func(*Message)

// MessageResponseSet stores a set of unique actions.  Comparison is based on function pointer identity.
type MessageHandlerSet map[string]MessageHandler

func getMessageHandlerKey(h MessageHandler) string {
	return fmt.Sprintf("%v", h)
}

// Add puts a new MessageHandler into the set.  Warns asynchronously if r is already in the set.
func (mhs MessageHandlerSet) Add(h MessageHandler) {
	key := getMessageHandlerKey(h)
	if _, ok := mhs[key]; ok {
		warn(ErrDuplicateHandler)
		return
	}
	mhs[key] = h
}

// Remove ensures that MessageHandler "r" is no longer present in the MessageHandlerSet
func (mhs MessageHandlerSet) Remove(h MessageHandler) {
	key := getMessageHandlerKey(h)
	// if key is not there, doesn't matter
	delete(mhs, key)
}

type MessageAgent struct {
	Hub *Hub

	// Parser overrides the default message parsing behavior if defined.  Default is nil
	Parser MessageParser
	// Delegate allows another object to act as the Recipient of messages from this agent if defined.
	// Default is nil.
	Delegate interface{}

	subscriptions map[string]MessageHandlerSet
	conn          *websocket.Conn
	sendText      chan []byte
	sendBinary    chan []byte
}

func NewMessageAgent(w http.ResponseWriter, r *http.Request) (*MessageAgent, error) {
	// TODO tj - do message agents really need to be on a hub?  I think we do it for consistency.
	// Hub can also act as family
	return DefaultHub().NewMessageAgent(w, r)
}

// MessageAgent implements MessageDelegate
func (agent *MessageAgent) MessageAgent() *MessageAgent {
	return agent
}

func (agent *MessageAgent) Subscribe(kind string, do MessageHandler) {
	if _, ok := agent.subscriptions[kind]; !ok {
		agent.subscriptions[kind] = make(MessageHandlerSet)
	}
	agent.subscriptions[kind].Add(do)
}

func (agent *MessageAgent) Unsubscribe(kind string, do MessageHandler) {
	if handlers, ok := agent.subscriptions[kind]; ok {
		handlers.Remove(do)
	} else {
		warn(ErrNoSubscriptions)
	}
}

func (agent *MessageAgent) PushMessage(m []byte, mtype int) {
	switch mtype {
	case websocket.BinaryMessage:
		agent.sendBinary <- m
	case websocket.TextMessage:
		agent.sendText <- m
	default:
		throw(ErrBadMessageType)
	}
}

func (agent *MessageAgent) StopListening(kind string) {
	delete(agent.subscriptions, kind)
}

func (agent *MessageAgent) ParseText(m []byte) (*ParsedMessage, error) {
	if agent.Parser != nil {
		return agent.Parser.ParseText(m)
	}
	return DefaultTextParser(m)
}

func (agent *MessageAgent) ParseBinary(m []byte) (*ParsedMessage, error) {
	if agent.Parser != nil {
		return agent.Parser.ParseBinary(m)
	}
	return nil, errNotYetImplemented
}

func (agent *MessageAgent) connect(w http.ResponseWriter, r *http.Request) error {
	upgrader := websocket.Upgrader{
		HandshakeTimeout: HandshakeTimeout,
		ReadBufferSize:   ReadBufferSize,
		WriteBufferSize:  WriteBufferSize,
	}
	// TODO add response header?
	var responseHeader http.Header
	conn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return err
	}
	agent.conn = conn
	go agent.startReading()
	go agent.startWriting()

	return nil
}

func (agent *MessageAgent) startReading() {
	defer agent.cleanup()

	agent.conn.SetReadLimit(ReadLimit)
	agent.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	agent.conn.SetPongHandler(agent.handlePong)
	agent.conn.SetCloseHandler(agent.handleClose)

	for {
		mtype, m, err := agent.conn.ReadMessage()
		if err != nil {
			// TODO this doesn't really throw, or raise - it just reports; rename
			throw(err)
			agent.cleanup()
			return
		}
		agent.acceptMessage(mtype, m)
	}
}

func (agent *MessageAgent) acceptMessage(mtype int, m []byte) {
	var (
		p   *ParsedMessage
		err error
	)
	switch mtype {
	case websocket.BinaryMessage:
		p, err = agent.ParseBinary(m)
		if err != nil {
			throw(err)
			return
		}
	case websocket.TextMessage:
		p, err = agent.ParseText(m)
		if err != nil {
			throw(err)
			return
		}
	default:
		throw(ErrUnparseableMessage)
		return
	}
	message := &Message{}
	message.Data = p.Value
	message.Kind = p.Kind
	message.Raw = p.Raw
	message.Source = agent
	if agent.Delegate != nil {
		message.Recipient = agent.Delegate
	} else {
		message.Recipient = agent
	}

	agent.handle(message)
}

func (agent *MessageAgent) handle(m *Message) {
	if handlers, ok := agent.subscriptions[m.Kind]; ok {
		for _, h := range handlers {
			h(m)
		}
		return
	}

	warn(ErrNoSubscribers)
}

func (agent *MessageAgent) startWriting() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		warn(ErrMessageConnectionLost)
		ticker.Stop()
		agent.cleanup()
	}()

	for {
		select {
		case message, ok := <-agent.sendText:
			if !ok {
				return
			}
			agent.doWrite(websocket.BinaryMessage, message)
		case message, ok := <-agent.sendBinary:
			if !ok {
				return
			}
			agent.doWrite(websocket.TextMessage, message)
		case <-ticker.C:
			if err := agent.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(Timeout)); err != nil {
				return
			}
		}
	}
}

func (agent *MessageAgent) doWrite(mtype int, m []byte) {
	agent.conn.SetWriteDeadline(time.Now().Add(Timeout))
	if err := agent.conn.WriteMessage(mtype, m); err != nil {
		throw(err)
	}
}

func (agent *MessageAgent) cleanup() {
	if _, ok := <-agent.sendBinary; ok {
		close(agent.sendBinary)
	}
	if _, ok := <-agent.sendText; ok {
		close(agent.sendText)
	}

	// TODO tj handle abnormal closure
	agent.conn.WriteControl(websocket.CloseNormalClosure, []byte{}, time.Now().Add(Timeout))
	agent.conn.Close()
}

func (agent *MessageAgent) handlePong(pong string) error {
	agent.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	return nil
}

// TODO tj
func (agent *MessageAgent) handleClose(code int, text string) error {
	agent.cleanup()
	return nil
}
