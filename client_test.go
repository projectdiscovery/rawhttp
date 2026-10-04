package rawhttp

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	rawclient "github.com/projectdiscovery/rawhttp/client"
	"github.com/projectdiscovery/stringsutil"
)

func getTestHttpServer(timeout time.Duration) *httptest.Server {
	var ts *httptest.Server
	router := httprouter.New()
	router.GET("/rawhttp", httprouter.Handle(func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		time.Sleep(timeout)
	}))
	ts = httptest.NewServer(router)
	return ts
}

// run with go test -timeout 45s -run ^TestDialDefaultTimeout$ github.com/projectdiscovery/rawhttp
func TestDialDefaultTimeout(t *testing.T) {
	timeout := 30 * time.Second
	ts := getTestHttpServer(45 * time.Second)
	defer ts.Close()

	startTime := time.Now()
	client := NewClient(DefaultOptions)
	_, err := client.DoRaw("GET", ts.URL, "/rawhttp", nil, nil)
	if !stringsutil.ContainsAny(err.Error(), "i/o timeout") || time.Now().Before(startTime.Add(timeout)) {
		t.Error("default timeout error")
	}
}

func TestDialWithCustomTimeout(t *testing.T) {
	timeout := 5 * time.Second
	ts := getTestHttpServer(10 * time.Second)
	defer ts.Close()

	startTime := time.Now()
	client := NewClient(DefaultOptions)
	options := DefaultOptions
	options.Timeout = timeout
	_, err := client.DoRawWithOptions("GET", ts.URL, "/rawhttp", nil, nil, options)
	if !stringsutil.ContainsAny(err.Error(), "i/o timeout") || time.Now().Before(startTime.Add(timeout)) {
		t.Error("custom timeout error")
	}
}

type cleanupConn struct {
	writeErr   error
	readErr    error
	closeErr   error
	response   *rawclient.Response
	closeCalls int
}

func (c *cleanupConn) WriteRequest(*rawclient.Request) error { return c.writeErr }
func (c *cleanupConn) ReadResponse(bool) (*rawclient.Response, error) {
	return c.response, c.readErr
}
func (c *cleanupConn) Close() error {
	c.closeCalls++
	return c.closeErr
}
func (c *cleanupConn) SetDeadline(time.Time) error      { return nil }
func (c *cleanupConn) SetReadDeadline(time.Time) error  { return nil }
func (c *cleanupConn) SetWriteDeadline(time.Time) error { return nil }
func (c *cleanupConn) Release()                         {}

type cleanupDialer struct {
	Dialer
	conn Conn
}

func (d *cleanupDialer) Dial(string, string, *Options) (Conn, error) {
	return d.conn, nil
}

func TestRequestErrorsCloseConnection(t *testing.T) {
	requestErr := errors.New("request failed")
	closeErr := errors.New("close failed")
	tests := []struct {
		name string
		conn func() cleanupConn
		err  error
	}{
		{name: "write", conn: func() cleanupConn { return cleanupConn{writeErr: requestErr} }, err: requestErr},
		{name: "read", conn: func() cleanupConn { return cleanupConn{readErr: requestErr} }, err: requestErr},
		{
			name: "gzip",
			conn: func() cleanupConn {
				return cleanupConn{response: &rawclient.Response{
					Version: rawclient.HTTP_1_1,
					Status:  rawclient.Status{Code: http.StatusOK, Reason: "OK"},
					Headers: []rawclient.Header{{Key: "Content-Encoding", Value: "gzip"}},
					Body:    strings.NewReader("invalid gzip header"),
				}}
			},
			err: gzip.ErrHeader,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, connCloseErr := range []error{nil, closeErr} {
				conn := tt.conn()
				conn.closeErr = connCloseErr
				c := &Client{dialer: &cleanupDialer{conn: &conn}, Options: &Options{}}
				resp, err := c.DoRaw("GET", "http://example.com/", "", nil, nil)
				if resp != nil || !errors.Is(err, tt.err) {
					t.Fatalf("response = %v, error = %v, want nil and %v", resp, err, tt.err)
				}
				if conn.closeCalls != 1 {
					t.Errorf("Close called %d times, want 1 (close error: %v)", conn.closeCalls, connCloseErr)
				}
			}
		})
	}
}

func TestResponseBodyOwnsConnection(t *testing.T) {
	conn := &cleanupConn{response: &rawclient.Response{
		Version: rawclient.HTTP_1_1,
		Status:  rawclient.Status{Code: http.StatusOK, Reason: "OK"},
		Body:    strings.NewReader("response body"),
	}}
	c := &Client{dialer: &cleanupDialer{conn: conn}, Options: &Options{}}
	resp, err := c.DoRaw("GET", "http://example.com/", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if conn.closeCalls != 0 {
		t.Fatal("connection closed before the response body was read")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "response body" {
		t.Fatalf("body = %q, error = %v", body, err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if conn.closeCalls != 1 {
		t.Errorf("Close called %d times, want 1", conn.closeCalls)
	}
}
