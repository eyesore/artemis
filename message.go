package artemis

import "fmt"

// MessageParser parses byte streams into MessageDataGetters
type MessageParser interface {
	ParseText([]byte) (MessageDataGetter, error)
	ParseBinary([]byte) (MessageDataGetter, error)
}

// MessageListener manages the susbscriptions of one or more MessageResponders
type MessageListener interface {
	OnMessage(string, MessageResponse)
	OffMessage(string, MessageResponse)
}

// MessageResponder can respond to a received message by doing an MessageResponse
type MessageResponder interface {
	MessageListener
	// Client returns the client that received the message being responded to.
	// All message responders must have some way of keeping track of which client received the message,
	// in order to pass it to MessageResponse
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
	PushMessage([]byte, int)
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

// MessageResponse is a function that is executed in response to a message.
// TODO - remove client arg - move to MessageDataGetter
type MessageResponse func(*Client, MessageDataGetter)

// MessageResponseSet stores a set of unique actions.  Comparison is based on function pointer identity.
type MessageResponseSet map[string]MessageResponse

func getMessageResponseKey(r MessageResponse) string {
	return fmt.Sprintf("%v", r)
}

// Add adds a new MessageResponse to the action set.  Returns error if the MessageResponse is already in the set.
func (rs MessageResponseSet) Add(r MessageResponse) {
	key := getMessageResponseKey(r)
	if _, ok := rs[key]; ok {
		warn(ErrDuplicateAction)
		return
	}
	rs[key] = r
	return
}

// Remove ensures that MessageResponse "a" is no longer present in the MessageResponseSet
func (rs MessageResponseSet) Remove(r MessageResponse) {
	key := getMessageResponseKey(r)
	// if key is not there, doesn't matter
	delete(rs, key)
}
