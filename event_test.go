package artemis

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

var (
	// default test timeout
	deadline        = 3 * time.Second
	defaultRequest  = httptest.NewRequest("GET", "/doesnotmatter", nil)
	defaultResponse = NewWsRecorder()

	errTimeoutWaitingForValue = errors.New("Test timed out while waiting for value")
)

func init() {
	defaultRequest.Header.Add("Connection", "upgrade")
	defaultRequest.Header.Add("Upgrade", "websocket")
	defaultRequest.Header.Add("Sec-Websocket-Version", "13")
	defaultRequest.Header.Add("Sec-Websocket-Key", "this is a test")
}

// TODO break all this out
type WsRecorder struct {
	*httptest.ResponseRecorder
	in   *bufio.Reader
	out  *bufio.Writer
	conn *MockConn
}

func NewWsRecorder() *WsRecorder {
	w := WsRecorder{
		httptest.NewRecorder(),
		bufio.NewReader(bytes.NewBuffer(nil)),
		nil,
		NewMockConn(),
	}
	w.out = bufio.NewWriter(w.Body)
	return &w
}

func (w *WsRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(w.in, w.out), nil
}

type MockAddr struct {
	NetworkString string
	AddrString    string
}

func (a *MockAddr) Network() string {
	return a.NetworkString
}

func (a *MockAddr) String() string {
	return a.AddrString
}

type MockConn struct {
	*bytes.Buffer
	addr *MockAddr
}

func NewMockConn() *MockConn {
	conn := MockConn{
		bytes.NewBuffer([]byte{}),
		&MockAddr{
			"tcp",
			"127.0.0.1",
		},
	}

	return &conn
}

func (mc *MockConn) Close() error {
	return nil
}

func (mc *MockConn) LocalAddr() net.Addr {
	return mc.addr
}

func (mc *MockConn) RemoteAddr() net.Addr {
	return mc.addr
}

func (mc *MockConn) SetDeadline(t time.Time) error {
	mc.SetReadDeadline(t)
	mc.SetWriteDeadline(t)
	return nil
}

func (mc *MockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (mc *MockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func createTestClient(t *testing.T, id string, h *Hub) *Client {
	c, err := NewClient(id, defaultRequest, defaultResponse, h)
	if err != nil {
		t.Fatal("Unable to create test client with id: ", id, "- ", err)
	}

	return c
}

func createTestFamily(t *testing.T, id string, h *Hub) *Family {
	return NewFamily(h)
}

func createTestHub(t *testing.T, id string) *Hub {
	h, err := NewHub(id)
	if err != nil {
		t.Fatal("Unable to create test hub with id: ", id)
	}

	return h
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
}

func TestSingleDefaultHub(t *testing.T) {
	c1 := createTestClient(t, "c1", nil)
	c2 := createTestClient(t, "c2", nil)
	eventName := "testEvent"
	valueC := make(chan interface{})

	c1.OnEvent(eventName, func(r EventResponder, dg DataGetter) {
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
	c1 := createTestClient(t, "c1", nil)
	eventName := "testEvent"
	valueC := make(chan interface{})

	c1.OnEvent(eventName, func(r EventResponder, dg DataGetter) {
		valueC <- dg
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
	if d := value.(DataGetter).Data().(struct {
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
	c1 := createTestClient(t, "c1", h1)
	c2 := createTestClient(t, "c2", h2)
	h1Chan := make(chan interface{}, 5)
	h2Chan := make(chan interface{}, 5)
	eventName := "sameForBothHubs"

	c1.OnEvent(eventName, func(r EventResponder, dg DataGetter) {
		h1Chan <- 1
	})
	c2.OnEvent(eventName, func(r EventResponder, dg DataGetter) {
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
	c1 := createTestClient(t, "c1", nil)
	c2 := createTestClient(t, "c2", nil)
	ch := make(chan interface{})
	eventName := "testEvent"

	c1.Join(f1)
	f1.OnEvent(eventName, func(r EventResponder, dg DataGetter) {
		ch <- r
	})
	c2.Join(f1)

	c1.Trigger(eventName, nil)
	for i := 0; i < 2; i++ {
		// should receive one value from each client listening
		data, err := waitForValueOrTimeout(ch, deadline)
		if err != nil {
			t.Error("Timed out waiting for ", eventName)
			continue
		}
		t.Logf("Heard event (%v) from %s", i+1, data.(*Client).ID)
	}

	cleanup()
}

func TestFamilyLeaveUnsubscribe(t *testing.T) {
	c1 := createTestClient(t, "c1", nil)
	// not necessessary to fire from a separate client, just testing this case
	c2 := createTestClient(t, "c2", nil)
	f1 := createTestFamily(t, "f1", nil)
	f2 := createTestFamily(t, "f2", nil)
	f3 := createTestFamily(t, "f3", nil)
	e1 := "e1"
	e2 := "e2"
	e3 := "e3"
	ch := make(chan interface{})
	cb1 := func(r EventResponder, dg DataGetter) {
		ch <- e1
	}
	cb2 := func(r EventResponder, dg DataGetter) {
		ch <- e2
	}
	cb3 := func(r EventResponder, dg DataGetter) {
		ch <- e3
	}

	c1.Join(f1, f2)

	f1.OnEvent(e1, cb1)
	f2.OnEvent(e2, cb2)
	f3.OnEvent(e3, cb3)
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
	c1 := createTestClient(t, "c1", nil)
	f1 := createTestFamily(t, "f1", nil)
	f2 := createTestFamily(t, "f2", nil)
	ch := make(chan interface{})

	c1.Join(f1, f2)
	eventName := "testEvent"
	cb1 := func(r EventResponder, dg DataGetter) {
		ch <- 1
	}

	f1.OnEvent(eventName, cb1)
	f2.OnEvent(eventName, cb1)

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
	c1 := createTestClient(t, "c1", nil)
	c2 := createTestClient(t, "c2", nil)
	c3 := createTestClient(t, "c3", nil)
	c4 := createTestClient(t, "c4", nil)
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
			if err != nil {
				t.Logf("Error occurred waiting for event named '%s'", eventName)
				t.Error(err)
				continue
			}
			id := value.(*Client).ID
			for _, client := range clients {
				if id == client {
					t.Errorf("Non-subscribed entity: %s received event %s", id, eventName)
				}
			}
		}
	}

	respondWithSelf := func(r EventResponder, dg DataGetter) {
		ch <- r
	}

	f1.OnEvent(f1Event, respondWithSelf)
	f2.OnEvent(f2Event, respondWithSelf)

	// does not have to be subscriber to trigger
	c3.Trigger(f1Event, nil)
	assertDidNotFire(f1Event, "c3", "c4")
	c1.Trigger(f2Event, nil)
	assertDidNotFire(f2Event, "c1", "c2")

	c1.OnEvent(c1and3Event, respondWithSelf)
	c3.OnEvent(c1and3Event, respondWithSelf)
	c2.OnEvent(c2and3Event, respondWithSelf)
	c3.OnEvent(c2and3Event, respondWithSelf)

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
	c1 := createTestClient(t, "c1", nil)
	ch1 := make(chan interface{})
	e1 := "e1"
	cb1 := func(r EventResponder, dg DataGetter) {
		ch1 <- 1
	}

	c1.OnEvent(e1, cb1)
	c1.Trigger(e1, nil)
	if _, err := waitForValueOrTimeout(ch1, deadline); err != nil {
		t.Error(err)
	}
	c1.OffEvent(e1, cb1)
	c1.Trigger(e1, nil)
	if _, err := waitForValueOrTimeout(ch1, deadline); err != errTimeoutWaitingForValue {
		t.Error(err)
	}
}
