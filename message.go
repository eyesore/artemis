package artemis

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
