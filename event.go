package artemis

import "fmt"

type Event struct {
	Kind      string
	Data      interface{}
	Recipient interface{}
	Source    interface{}
}

func newEvent(kind string, data DataGetter) *Event {
	e := &Event{}

	e.Kind = kind
	if data != nil {
		e.Data = data.Data()
	} else {
		e.Data = nil
	}

	return e
}

type DataGetter interface {
	Data() interface{}
}

type EventData struct {
	data interface{}
}

func (ed *EventData) Data() interface{} {
	return ed.data
}

// EventHandler is a function that handles events.
type EventHandler func(*Event)

type EventHandlerSet map[string]EventHandler

// EventResponderSet is predicated on being able to distinguish between functions to prevent
// duplicate adds and to allow removal.  This proves difficult to do.
// The go language spec states that functions are not comparable -
// therefore, there is no guarantee that this technique will work in the future, or at all
// Link: http://stackoverflow.com/a/42147285/1375316
// TODO compare interfaces instead of fns?
func getEventHandlerKey(eh EventHandler) string {
	return fmt.Sprintf("%v", eh)
}

func (ehs EventHandlerSet) Add(h EventHandler) {
	key := getEventHandlerKey(h)
	if _, ok := ehs[key]; ok {
		warn(ErrDuplicateHandler)
		return
	}
	ehs[key] = h
}

func (ehs EventHandlerSet) Remove(h EventHandler) {
	key := getEventHandlerKey(h)
	// if key is not there, doesn't matter
	delete(ehs, key)
}

type EventAgent struct {
	Hub *Hub

	// Delegate will become the recipient on Event objects received if set.
	Delegate interface{}

	events        chan *Event
	ready         bool
	subscriptions map[string]EventHandlerSet
}

func NewEventAgent() *EventAgent {
	return DefaultHub().NewEventAgent()
}

func (agent *EventAgent) EventAgent() *EventAgent {
	return agent
}

func (agent *EventAgent) Subscribe(kind string, do EventHandler) {
	if !agent.ready {
		go agent.listen()
	}
	if _, ok := agent.subscriptions[kind]; !ok {
		agent.subscriptions[kind] = make(EventHandlerSet)
	}
	agent.subscriptions[kind].Add(do)
	agent.Hub.subscribe(kind, agent.events)
}

func (agent *EventAgent) Unsubscribe(kind string, do EventHandler) {
	if actions, ok := agent.subscriptions[kind]; ok {
		actions.Remove(do)
	}
	agent.Hub.unsubscribe(kind, agent.events)
}

func (agent *EventAgent) listen() {
	// TODO tj test that this is cleaned up when garbage is collected
	defer close(agent.events)
	agent.ready = true
	for {
		ev, ok := <-agent.events
		if !ok {
			break
		}
		if agent.Delegate != nil {
			ev.Recipient = agent.Delegate
		} else {
			ev.Recipient = agent
		}
		if actions, ok := agent.subscriptions[ev.Kind]; ok {
			for _, do := range actions {
				do(ev)
			}
		}
	}

	warn(ErrEventChannelHasClosed)
}
