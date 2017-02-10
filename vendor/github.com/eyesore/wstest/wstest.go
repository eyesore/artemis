package wstest

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"time"
)

type WsRecorder struct {
	*httptest.ResponseRecorder
	conn *MockConn
}

func NewRecorder() *WsRecorder {
	w := WsRecorder{
		httptest.NewRecorder(),
		nil,
	}
	// reader := bytes.NewReader([]byte{}) // this is reading NOTHING
	// TJ - this is very weird - check it out - does it work?
	read, _ := io.Pipe()
	w.conn = NewMockConn(read, w)

	return &w
}

func (w *WsRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	readPipe, writePipe := io.Pipe()
	reader := bufio.NewReader(readPipe)
	writer := bufio.NewWriter(writePipe)
	w.conn.HijackedPipe = bufio.NewReadWriter(reader, writer)

	return w.conn, w.conn.HijackedPipe, nil
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
	Reader       io.Reader
	Writer       io.Writer
	HijackedPipe *bufio.ReadWriter
	addr         *MockAddr
}

func NewMockConn(r io.Reader, w io.Writer) *MockConn {
	conn := MockConn{
		r,
		w,
		nil,
		&MockAddr{
			"tcp",
			"127.0.0.1",
		},
	}

	return &conn
}

func (mc *MockConn) InsertMessage(b []byte) (int, error) {
	return mc.HijackedPipe.Write(b)
}

func (mc *MockConn) Close() error {
	// if err := mc.Reader.Close(); err != nil {
	// 	return err
	// }
	// if err := mc.Writer.Close(); err != nil {
	// 	return err
	// }

	return nil
}

func (mc *MockConn) Read(data []byte) (int, error)  { return mc.Reader.Read(data) }
func (mc *MockConn) Write(data []byte) (int, error) { return mc.Writer.Write(data) }

func (mc *MockConn) LocalAddr() net.Addr  { return mc.addr }
func (mc *MockConn) RemoteAddr() net.Addr { return mc.addr }

func (mc *MockConn) SetDeadline(t time.Time) error      { return nil }
func (mc *MockConn) SetReadDeadline(t time.Time) error  { return nil }
func (mc *MockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestRequest(target string) *http.Request {
	r := httptest.NewRequest("GET", target, nil)
	r.Header.Add("Connection", "upgrade")
	r.Header.Add("Upgrade", "websocket")
	r.Header.Add("Sec-Websocket-Version", "13")
	r.Header.Add("Sec-Websocket-Key", "this is a test")

	return r
}
