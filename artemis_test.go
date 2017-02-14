package artemis

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var (
	// default test timeout
	deadline         = 3 * time.Second
	testServerPort   = "8081"
	testPath         = "testws"
	testJSONObj      = []byte(`{"kind":"testMessage","data":{"item1":"thing","item2":"thing2"}}`)
	stopChan         = make(chan bool)
	connectedClients = make(chan interface{}, 5)

	errTimeoutWaitingForValue = errors.New("Test timed out while waiting for value")
)

// TODO how confusing is this signature?  the server is a client and the client is a conn
func createTestClients(t *testing.T, id string, h *Hub) (client *websocket.Conn, server *Client) {
	// TODO header?
	u := url.URL{Scheme: "ws", Host: "localhost:" + testServerPort, Path: testPath}
	if h != nil {
		u.RawQuery = "hub_id=" + h.ID
	}
	client, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal("Failed to get ws client connection: ", err)
	}

	serverInterface, err := waitForValueOrTimeout(connectedClients, 5*time.Second)
	if err != nil {
		t.Fatal("Failed to get ws server connection: ", err)
	}
	server = serverInterface.(*Client)
	server.ID = id

	return
}

func createTestFamily(t *testing.T, id string, h *Hub) *Family {
	if h == nil {
		h = DefaultHub()
	}
	return h.NewFamily(id)
}

func createTestHub(t *testing.T, id string) *Hub {
	h, err := NewHub(id)
	if err != nil {
		t.Fatal("Unable to create test hub with id=", id, " Err:", err)
	}

	return h
}

func createTestServer() error {
	http.HandleFunc("/testws", func(w http.ResponseWriter, r *http.Request) {
		var (
			hub *Hub
			err error
		)
		query := r.URL.Query()
		hubID := query.Get("hub_id")
		if hubID == "" {
			hub = DefaultHub()
		} else {
			// rare case where the error REALLY doesn't matter and IS GOING TO BE THROWN
			hub, _ = NewHub(hubID)
		}
		// set up client and pass to client creation
		c, err := hub.NewClient(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		connectedClients <- c
	})

	l, err := net.Listen("tcp", fmt.Sprintf(":%s", testServerPort))
	if err != nil {
		return err
	}

	go waitForStopSignal(l)
	go http.Serve(l, nil)

	return nil
}

func waitForStopSignal(l net.Listener) {
	<-stopChan
	l.Close()
	close(stopChan)
	close(connectedClients)
}

func waitForValueOrTimeout(c chan interface{}, wait time.Duration) (interface{}, error) {
	select {
	case value := <-c:
		return value, nil
	case <-time.After(wait):
		return nil, errTimeoutWaitingForValue
	}
}

func cleanup() {
	defaultHub = nil
	hubs = make(map[string]*Hub)
}

func TestMain(m *testing.M) {
	// TODO testgroups for messaging and events separately?
	err := createTestServer()
	if err != nil {
		os.Exit(1)
	}
	defer func() {
		stopChan <- true
	}()

	os.Exit(m.Run())
}

// EVENTS

func TestSingleDefaultHub(t *testing.T) {
	_, c1 := createTestClients(t, "c1", nil)
	_, c2 := createTestClients(t, "c2", nil)
	eventName := "testEvent"
	valueC := make(chan interface{})

	c1.Events.Subscribe(eventName, func(e *Event) {
		valueC <- 1
	})

	c2.Trigger(eventName, nil)
	value, err := waitForValueOrTimeout(valueC, deadline)
	if err != nil {
		t.Fatal("Timed out waiting for testEvent")
	}
	if value.(int) != 1 {
		t.Fatal("Got wrong value from channel.") // should never happen...
	}
	cleanup()
}

func TestDataContent(t *testing.T) {
	// also test c1 firing and receiving event
	_, c1 := createTestClients(t, "c1", nil)
	eventName := "testEvent"
	valueC := make(chan interface{})

	c1.Events.Subscribe(eventName, func(e *Event) {
		valueC <- e
	})
	data := EventData{
		struct {
			number int
			text   string
		}{2, "test"},
	}

	c1.Trigger(eventName, &data)
	value, err := waitForValueOrTimeout(valueC, deadline)
	if err != nil {
		t.Fatal("Timed out waiting for testEvent")
	}
	if d := value.(*Event).Data.(struct {
		number int
		text   string
	}); d.number != 2 || d.text != "test" {
		t.Fatal("Values passed in event data did not match expected.")
	}

	cleanup()
}

func TestHubIsolation(t *testing.T) {
	h1 := createTestHub(t, "h1")
	h2 := createTestHub(t, "h2")
	_, c1 := createTestClients(t, "c1", h1)
	_, c2 := createTestClients(t, "c2", h2)
	h1Chan := make(chan interface{}, 5)
	h2Chan := make(chan interface{}, 5)
	eventName := "sameForBothHubs"

	c1.Events.Subscribe(eventName, func(e *Event) {
		h1Chan <- 1
	})
	c2.Events.Subscribe(eventName, func(e *Event) {
		h2Chan <- 1
	})

	c1.Trigger(eventName, nil)
	if value, err := waitForValueOrTimeout(h2Chan, deadline); err != errTimeoutWaitingForValue {
		t.Error("Expected no event on h2 - should have timed out, but did sent value: ", value)
	}
	if _, err := waitForValueOrTimeout(h1Chan, deadline); err != nil {
		t.Error(err)
	}

	c2.Trigger(eventName, nil)
	if value, err := waitForValueOrTimeout(h1Chan, deadline); err != errTimeoutWaitingForValue {
		t.Error("Expected no event on h1 - should have timed out, but received event with value: ", value)
	}
	if _, err := waitForValueOrTimeout(h2Chan, deadline); err != nil {
		t.Error(err)
	}
	cleanup()
}

func TestFamilyResponse(t *testing.T) {
	f1 := createTestFamily(t, "f1", nil)
	_, c1 := createTestClients(t, "c1", nil)
	_, c2 := createTestClients(t, "c2", nil)
	ch := make(chan interface{})
	eventName := "testEvent"

	c1.Join(f1)
	f1.Events.Subscribe(eventName, func(e *Event) {
		ch <- e
	})
	c2.Join(f1)

	c1.Trigger(eventName, nil)
	for i := 0; i < 2; i++ {
		// should receive one value from each client listening
		event, err := waitForValueOrTimeout(ch, deadline)
		if err != nil {
			t.Error("Timed out waiting for ", eventName)
			continue
		}
		t.Logf("Heard event (%v) from %s", i+1, event.(*Event).Recipient.(*Client).ID)
	}

	cleanup()
}

func TestFamilyLeaveUnsubscribe(t *testing.T) {
	_, c1 := createTestClients(t, "c1", nil)
	// not necessessary to fire from a separate client, just testing this case
	_, c2 := createTestClients(t, "c2", nil)
	f1 := createTestFamily(t, "f1", nil)
	f2 := createTestFamily(t, "f2", nil)
	f3 := createTestFamily(t, "f3", nil)
	e1 := "e1"
	e2 := "e2"
	e3 := "e3"
	ch := make(chan interface{})
	cb1 := func(e *Event) {
		ch <- e1
	}
	cb2 := func(e *Event) {
		ch <- e2
	}
	cb3 := func(e *Event) {
		ch <- e3
	}

	c1.Join(f1, f2)

	f1.Events.Subscribe(e1, cb1)
	f2.Events.Subscribe(e2, cb2)
	f3.Events.Subscribe(e3, cb3)
	c1.Join(f3)

	c2.Trigger(e1, nil)
	if e1Val, err := waitForValueOrTimeout(ch, deadline); err != nil || e1Val != e1 {
		t.Error("e1 not received correctly")
	}
	c2.Trigger(e2, nil)
	if e2Val, err := waitForValueOrTimeout(ch, deadline); err != nil || e2Val != e2 {
		t.Error("e2 not received correctly")
	}
	c2.Trigger(e3, nil)
	if e3Val, err := waitForValueOrTimeout(ch, deadline); err != nil || e3Val != e3 {
		t.Error("e3 not received correctly")
	}

	c1.Leave(f2)
	c2.Trigger(e1, nil)
	if e1Val, err := waitForValueOrTimeout(ch, deadline); err != nil || e1Val != e1 {
		t.Error("e1 not received correctly after leaving f2")
	}
	c2.Trigger(e3, nil)
	if e3Val, err := waitForValueOrTimeout(ch, deadline); err != nil || e3Val != e3 {
		t.Error("e3 not received correctly after leaving f2")
	}
	c2.Trigger(e2, nil)
	if _, err := waitForValueOrTimeout(ch, deadline); err != errTimeoutWaitingForValue {
		t.Error("e2 should have timed out with no listeners for the event.")
	}
	cleanup()
}

// current expected behavior is for the listener to only be added once.
func TestDifferentFamilySameListener(t *testing.T) {
	_, c1 := createTestClients(t, "c1", nil)
	f1 := createTestFamily(t, "f1", nil)
	f2 := createTestFamily(t, "f2", nil)
	ch := make(chan interface{})

	c1.Join(f1, f2)
	eventName := "testEvent"
	cb1 := func(e *Event) {
		ch <- 1
	}

	f1.Events.Subscribe(eventName, cb1)
	f2.Events.Subscribe(eventName, cb1)

	c1.Trigger(eventName, nil)
	if _, err := waitForValueOrTimeout(ch, deadline); err != nil {
		t.Fatal(err)
	}
	// second attempt should timeout
	if _, err := waitForValueOrTimeout(ch, deadline); err != errTimeoutWaitingForValue {
		t.Fatal("Expected to receive one event from c1, not 2")
	}
	cleanup()
}

func TestNonsubscribers(t *testing.T) {
	_, c1 := createTestClients(t, "c1", nil)
	_, c2 := createTestClients(t, "c2", nil)
	_, c3 := createTestClients(t, "c3", nil)
	_, c4 := createTestClients(t, "c4", nil)
	f1 := createTestFamily(t, "f1", nil)
	f2 := createTestFamily(t, "f2", nil)
	f1Event := "f1"
	f2Event := "f2"
	c1and3Event := "c1c3"
	c2and3Event := "c2c3"
	noneEvent := "none"
	ch := make(chan interface{})

	c1.Join(f1)
	c2.Join(f1)
	c3.Join(f2)
	c4.Join(f2)
	assertDidNotFire := func(eventName string, clients ...string) {
		for i := 0; i < 2; i++ {
			value, err := waitForValueOrTimeout(ch, deadline)
			event := value.(*Event)
			if err != nil {
				t.Logf("Error occurred waiting for event named '%s'", eventName)
				t.Error(err)
				continue
			}
			id := event.Recipient.(*Client).ID
			for _, client := range clients {
				if id == client {
					t.Errorf("Non-subscribed entity: %s received event %s", id, eventName)
				}
			}
		}
	}

	respondWithSelf := func(e *Event) {
		ch <- e
	}

	f1.Events.Subscribe(f1Event, respondWithSelf)
	f2.Events.Subscribe(f2Event, respondWithSelf)

	// does not have to be subscriber to trigger
	c3.Trigger(f1Event, nil)
	assertDidNotFire(f1Event, "c3", "c4")
	c1.Trigger(f2Event, nil)
	assertDidNotFire(f2Event, "c1", "c2")

	c1.Events.Subscribe(c1and3Event, respondWithSelf)
	c3.Events.Subscribe(c1and3Event, respondWithSelf)
	c2.Events.Subscribe(c2and3Event, respondWithSelf)
	c3.Events.Subscribe(c2and3Event, respondWithSelf)

	c4.Trigger(c1and3Event, nil)
	assertDidNotFire(c1and3Event, "c2", "c4")
	c1.Trigger(c2and3Event, nil)
	assertDidNotFire(c2and3Event, "c1", "c4")

	c4.Trigger(noneEvent, nil)
	_, err := waitForValueOrTimeout(ch, deadline)
	if err != errTimeoutWaitingForValue {
		t.Error("Unexpected value received for none event.")
	}
	cleanup()
}

func TestOffEvent(t *testing.T) {
	_, c1 := createTestClients(t, "c1", nil)
	ch1 := make(chan interface{})
	e1 := "e1"
	cb1 := func(e *Event) {
		ch1 <- 1
	}

	c1.Events.Subscribe(e1, cb1)
	c1.Trigger(e1, nil)
	if _, err := waitForValueOrTimeout(ch1, deadline); err != nil {
		t.Error(err)
	}
	c1.Events.Unsubscribe(e1, cb1)
	c1.Trigger(e1, nil)
	if _, err := waitForValueOrTimeout(ch1, deadline); err != errTimeoutWaitingForValue {
		t.Error(err)
	}
}

// family specific

func TestFamilyClientHubMismatch(t *testing.T) {
	t.Skip("Skipping ClientHubMismatch test.  Re-evaluating expected behavior")
	// h1 := createTestHub(t, "h1")
	// f1 := createTestFamily(t, "f1",  h1)
	// f2 := createTestFamily(t, "f2", nil)
	// f3 := createTestFamily(t, "f3", nil)
	// _, c1 := createTestClients(t, "c1", nil)

	// // err, _ := <-Errors, c1.Join(f1); err != ErrHubMismatch {
	// // 	t.Error("Expected hub mismatch error, but none returned.")
	// // }
	// if err := c1.Join(f2, f3); err != nil {
	// 	t.Error("Client should be able to join these families without issue.")
	// }

	// c1.Leave(f2)
	// c1.Leave(f3)

	// err := c1.Join(f1, f2, f3)
	// if err != ErrHubMismatch {
	// 	t.Error("Expected hub mismatch when joining all 3 families, but none returned.")
	// }
	// if c1.BelongsTo(f1) || !c1.BelongsTo(f2) || !c1.BelongsTo(f3) {
	// 	t.Error("Resulting family membership for c1 is not correct.")
	// }
	// cleanup()
}

func TestFamilyJoinLeave(t *testing.T) {
	_, c1 := createTestClients(t, "c1", nil)
	f1 := createTestFamily(t, "f1", nil)
	f2 := createTestFamily(t, "f2", nil)

	c1.Join(f1, f2)
	if !c1.BelongsTo(f1) || !c1.BelongsTo(f2) {
		t.Fatal("c1 did not correctly join families.")
	}

	c1.Leave(f2)
	if !c1.BelongsTo(f1) {
		t.Error("c1 should still belong to f1")
	}
	if c1.BelongsTo(f2) {
		t.Error("c1 should no longer belong to f2")
	}
	cleanup()
}

// MESSAGES

func TestOnMessage(t *testing.T) {
	incoming, c1 := createTestClients(t, "c1", nil)
	messageName := "testMessage"
	ch := make(chan interface{})

	c1.Messages.Subscribe(messageName, func(m *Message) {
		ch <- 1
	})
	err := incoming.WriteMessage(websocket.TextMessage, testJSONObj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waitForValueOrTimeout(ch, deadline); err != nil {
		t.Fatal(err)
	}
	cleanup()
}

func TestOffMessage(t *testing.T) {
	incoming, c1 := createTestClients(t, "c1", nil)
	messageName := "testMessage"
	ch := make(chan interface{})
	cb1 := func(m *Message) {
		ch <- 1
	}

	c1.Messages.Subscribe(messageName, cb1)
	err := incoming.WriteMessage(websocket.TextMessage, testJSONObj)
	if err != nil {
		t.Fatal("Problem writing to incoming connection: ", err)
	}
	if _, err = waitForValueOrTimeout(ch, deadline); err != nil {
		t.Fatal(err)
	}

	c1.Messages.Unsubscribe(messageName, cb1)
	err = incoming.WriteMessage(websocket.TextMessage, testJSONObj)
	if err != nil {
		t.Fatal("Problem writing to incoming connection: ", err)
	}
	if _, err = waitForValueOrTimeout(ch, deadline); err != errTimeoutWaitingForValue {
		t.Fatal("Listener should have been removed, but we got a value anyway.")
	}
}

func TestFamilyOnMessage(t *testing.T) {
	incoming, c1 := createTestClients(t, "c1", nil)
	f1 := createTestFamily(t, "f1", nil)
	messageName := "testMessage"
	ch := make(chan interface{})
	c1.Join(f1)

	f1.Messages.Subscribe(messageName, func(m *Message) {
		log.Print("got a message")
		log.Print(m)
		ch <- m.Recipient
	})
	err := incoming.WriteMessage(websocket.TextMessage, testJSONObj)
	if err != nil {
		t.Fatal(err)
	}
	data, err := waitForValueOrTimeout(ch, deadline)
	if err != nil {
		t.Fatal(err)
	}
	if data.(*Client).ID != "c1" {
		t.Fatal("unexpected client id returned from message")
	}
	cleanup()
}

func TestFamilyOnMessageRetro(t *testing.T) {
	incoming, c1 := createTestClients(t, "c1", nil)
	f1 := createTestFamily(t, "f1", nil)
	messageName := "testMessage"
	ch := make(chan interface{})

	f1.Messages.Subscribe(messageName, func(m *Message) {
		ch <- m.Recipient
	})
	c1.Join(f1)

	err := incoming.WriteMessage(websocket.TextMessage, testJSONObj)
	if err != nil {
		t.Fatal(err)
	}
	data, err := waitForValueOrTimeout(ch, deadline)
	if err != nil {
		t.Fatal(err)
	}
	if data.(*Client).ID != "c1" {
		t.Fatal("unexpected client id returned from message")
	}
	cleanup()
}

func TestFamilyOffMessage(t *testing.T) {
	incoming, c1 := createTestClients(t, "c1", nil)
	f1 := createTestFamily(t, "f1", nil)
	messageName := "testMessage"
	ch := make(chan interface{})
	cb1 := func(m *Message) {
		ch <- 1
	}
	c1.Join(f1)

	f1.Messages.Subscribe(messageName, cb1)
	err := incoming.WriteMessage(websocket.TextMessage, testJSONObj)
	if err != nil {
		t.Fatal("Problem writing to incoming connection: ", err)
	}
	if _, err = waitForValueOrTimeout(ch, deadline); err != nil {
		t.Fatal(err)
	}

	f1.Messages.Unsubscribe(messageName, cb1)
	err = incoming.WriteMessage(websocket.TextMessage, testJSONObj)
	if err != nil {
		t.Fatal("Problem writing to incoming connection: ", err)
	}
	if _, err = waitForValueOrTimeout(ch, deadline); err != errTimeoutWaitingForValue {
		t.Fatal("Listener should have been removed, but we got a value anyway.")
	}
}
