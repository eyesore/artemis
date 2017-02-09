package wstest

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"time"
)

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

func TestRequest(target string) *http.Request {
	r := httptest.NewRequest("GET", target, nil)
	r.Header.Add("Connection", "upgrade")
	r.Header.Add("Upgrade", "websocket")
	r.Header.Add("Sec-Websocket-Version", "13")
	r.Header.Add("Sec-Websocket-Key", "this is a test")

	return r
}
