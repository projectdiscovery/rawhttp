package rawhttp

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
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

func TestFollowHostRedirects(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		hits []string
	)

	record := func(server string, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		hits = append(hits, server+" "+r.URL.Path)
	}

	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		record("other", r)
	}))
	defer other.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("target", r)

		location, ok := map[string]string{
			"/to-path":       "/landing",
			"/to-self":       "http://" + r.Host + "/landing",
			"/to-https":      "https://" + r.Host + "/landing",
			"/to-other-port": other.URL + "/landing",
			"/to-other-host": "http://" + strings.Replace(r.Host, "127.0.0.1", "localhost", 1) + "/landing",
		}[r.URL.Path]
		if ok {
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusFound)
		}
	}))
	defer target.Close()

	sameHost := Options{FollowRedirects: true, FollowHostRedirects: true, MaxRedirects: 10, AutomaticHostHeader: true, Timeout: 5 * time.Second}

	tests := []struct {
		name       string
		path       string
		options    Options
		wantStatus int
		wantHits   []string
	}{
		{
			name: "relative location", path: "/to-path", options: sameHost,
			wantStatus: http.StatusOK, wantHits: []string{"target /to-path", "target /landing"},
		},
		{
			name: "absolute location on the same host", path: "/to-self", options: sameHost,
			wantStatus: http.StatusOK, wantHits: []string{"target /to-self", "target /landing"},
		},
		{
			name: "other scheme", path: "/to-https", options: sameHost,
			wantStatus: http.StatusFound, wantHits: []string{"target /to-https"},
		},
		{
			name: "other port", path: "/to-other-port", options: sameHost,
			wantStatus: http.StatusFound, wantHits: []string{"target /to-other-port"},
		},
		{
			name: "other hostname", path: "/to-other-host", options: sameHost,
			wantStatus: http.StatusFound, wantHits: []string{"target /to-other-host"},
		},
		{
			name: "FollowHostRedirects without FollowRedirects", path: "/to-self",
			options:    Options{FollowHostRedirects: true, MaxRedirects: 10, AutomaticHostHeader: true, Timeout: 5 * time.Second},
			wantStatus: http.StatusOK, wantHits: []string{"target /to-self", "target /landing"},
		},
		{
			name: "FollowRedirects alone", path: "/to-other-port",
			options:    Options{FollowRedirects: true, MaxRedirects: 10, AutomaticHostHeader: true, Timeout: 5 * time.Second},
			wantStatus: http.StatusOK, wantHits: []string{"target /to-other-port", "other /landing"},
		},
	}
	// The subtests share hits, so they run one at a time.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			hits = nil
			mu.Unlock()

			c := &Client{dialer: new(dialer), Options: &tt.options}
			headers := map[string][]string{"Authorization": {"Bearer secret"}}

			resp, err := c.DoRawWithOptions("GET", target.URL+tt.path, "", headers, nil, &tt.options)
			if err != nil {
				t.Fatal(err)
			}

			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}

			mu.Lock()
			defer mu.Unlock()

			if resp.StatusCode != tt.wantStatus || !slices.Equal(hits, tt.wantHits) {
				t.Errorf("status = %d, requests = %q; want %d, %q", resp.StatusCode, hits, tt.wantStatus, tt.wantHits)
			}
		})
	}
}

func TestDialsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url, protocol, addr string
		want                bool
	}{
		{"http://example.com/x", "http", "example.com:80", true},
		{"http://example.com:80/x", "http", "example.com:80", true},
		{"https://example.com/x", "https", "example.com:443", true},
		{"https://example.com:80/x", "http", "example.com:80", false},
		// Without "//" the parser keeps Scheme "https" but no host; such a URL
		// has always been dialed as plain HTTP.
		{"https:example.com/x", "http", ":80", true},
		{"http://example.com:8080/x", "http", "example.com:80", false},
		{"http://example.org/x", "http", "example.com:80", false},
		{"http://example.com:80@example.org/x", "http", "example.com:80", false},
		// Case folding would map these hosts onto ASCII ones (U+017F to s,
		// U+212A to k) while they resolve to different names.
		{"http://ſecret.com/x", "http", "secret.com:80", false},
		{"http://Key.com/x", "http", "key.com:80", false},
		{"http://EXAMPLE.com/x", "http", "example.com:80", false},
		{"landing", "http", "example.com:80", false},
		{"", "http", "example.com:80", false},
	}
	for _, tt := range tests {
		if got := dialsTo(tt.url, tt.protocol, tt.addr); got != tt.want {
			t.Errorf("dialsTo(%q, %q, %q) = %v, want %v", tt.url, tt.protocol, tt.addr, got, tt.want)
		}
	}
}
