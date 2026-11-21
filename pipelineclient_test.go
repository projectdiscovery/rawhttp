package rawhttp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	retryablehttp "github.com/projectdiscovery/retryablehttp-go" //nolint:depguard // Dor takes a retryablehttp.Request, so its test has to build one
)

func TestPipelineClientGet(t *testing.T) {
	tests := []struct {
		name  string
		start func(*httptest.Server)
	}{
		{name: "http", start: (*httptest.Server).Start},
		{name: "https", start: (*httptest.Server).StartTLS},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "ok")
			}))
			srv.Config.ErrorLog = log.New(io.Discard, "", 0)
			tt.start(srv)
			defer srv.Close()

			opts := DefaultPipelineOptions
			opts.Host = srv.Listener.Addr().String()
			client := NewPipelineClient(opts)

			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK || string(body) != "ok" {
				t.Fatalf("GET %s = %d %q, want 200 %q", srv.URL, resp.StatusCode, body, "ok")
			}
		})
	}
}

// A body larger than the write buffer is flushed before any read deadline is
// set, so only Timeout can stop a server that never answers the handshake.
func TestPipelineClientTLSHandshakeTimeout(t *testing.T) {
	ln := listen(t)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// read the ClientHello and never answer, until the client gives up
		_, _ = io.Copy(io.Discard, conn)
		_ = conn.Close()
	}()

	opts := DefaultPipelineOptions
	opts.Host = ln.Addr().String()
	opts.Timeout = 500 * time.Millisecond
	client := NewPipelineClient(opts)

	done := make(chan error, 1)
	go func() {
		_, err := client.Post("https://"+opts.Host, "text/plain", strings.NewReader(strings.Repeat("a", 8192)))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want handshake error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request still blocked 5s after a 500ms timeout")
	}
}

// A relative URL has no host to name, and an empty Host value would be written
// as a bare "Host" line.
func TestPipelineClientRelativeURLIsWellFormed(t *testing.T) {
	ln := listen(t)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		status := "200 OK"
		if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
			status = "400 Bad Request"
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 "+status+"\r\nContent-Length: 0\r\n\r\n")
		_ = conn.Close()
	}()

	opts := DefaultPipelineOptions
	opts.Host = ln.Addr().String()
	client := NewPipelineClient(opts)

	resp, err := client.Get("/x")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /x = %d, want 200: request was malformed", resp.StatusCode)
	}
}

func TestPipelineClientKeepsCallerHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Host)
	}))
	defer srv.Close()

	opts := DefaultPipelineOptions
	opts.Host = srv.Listener.Addr().String()
	client := NewPipelineClient(opts)

	const host = "custom.example"
	resp, err := client.DoRaw("GET", srv.URL, "", map[string][]string{"host": {host}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != host {
		t.Fatalf("GET %s = %d %q, want 200 %q", srv.URL, resp.StatusCode, body, host)
	}
}

// wireRequest is a request as a server frames it by RFC 9112: the body is
// Content-Length bytes, and none without that field.
type wireRequest struct {
	line   string
	length []string
	body   string
}

// readWireRequest reads one request from reader, or returns nil at the end of
// the stream.
func readWireRequest(reader *textproto.Reader) *wireRequest {
	line, err := reader.ReadLine()
	if err != nil {
		return nil
	}

	header, err := reader.ReadMIMEHeader()
	if err != nil {
		return nil
	}

	length := header["Content-Length"]

	var body []byte

	if len(length) > 0 {
		size, _ := strconv.Atoi(length[0])
		body = make([]byte, size)

		_, err = io.ReadFull(reader.R, body)
		if err != nil {
			return nil
		}
	}

	return &wireRequest{line: line, length: length, body: string(body)}
}

// serveWire reads requests from the first connection to listener until it
// closes, answers each with 200, and sends what it framed to got. A request
// line that is not valid gets 400, as net/http does.
func serveWire(listener net.Listener, got chan<- wireRequest) {
	defer close(got)

	conn, err := listener.Accept()
	if err != nil {
		return
	}

	defer func() { _ = conn.Close() }()

	reader := textproto.NewReader(bufio.NewReader(conn))

	for {
		req := readWireRequest(reader)
		if req == nil {
			return
		}

		got <- *req

		status := "200 OK"
		if !strings.HasPrefix(req.line, "POST /") {
			status = "400 Bad Request"
		}

		_, _ = io.WriteString(conn, "HTTP/1.1 "+status+"\r\nContent-Length: 0\r\n\r\n")
	}
}

// wireClient returns a client with one connection to a serveWire server, the
// URL to send to, and the channel of requests the server framed.
func wireClient(t *testing.T) (*PipelineClient, string, <-chan wireRequest) {
	t.Helper()

	listener := listen(t)
	got := make(chan wireRequest, 4)

	go serveWire(listener, got)

	opts := DefaultPipelineOptions
	opts.Host = listener.Addr().String()
	opts.MaxConnections = 1
	opts.Timeout = 5 * time.Second

	return NewPipelineClient(opts), "http://" + opts.Host + "/", got
}

func newRequest(t *testing.T, url string, body io.Reader) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, body)
	if err != nil {
		t.Fatal(err)
	}

	return req
}

func newRetryableRequest(t *testing.T, url, body string) *retryablehttp.Request {
	t.Helper()

	req, err := retryablehttp.NewRequest(http.MethodPost, url, body)
	if err != nil {
		t.Fatal(err)
	}

	return req
}

func checkOK(t *testing.T, resp *http.Response, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
}

// bodyCase sends one request with the body that bodyCases names.
type bodyCase struct {
	name string
	send func(t *testing.T, client *PipelineClient, url string) (*http.Response, error)
}

// bodyCases returns the ways to send body, which the server must read as the
// body of a request whose Content-Length is its length.
func bodyCases(body string) []bodyCase {
	length := strconv.Itoa(len(body))

	return []bodyCase{
		{"Post", func(_ *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			return client.Post(url, "text/plain", strings.NewReader(body))
		}},
		{"Do", func(t *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			t.Helper()

			return client.Do(newRequest(t, url, strings.NewReader(body)))
		}},
		{"Dor", func(t *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			t.Helper()

			return client.Dor(newRetryableRequest(t, url, body))
		}},
		{"Do with ContentLength -1", func(t *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			t.Helper()

			req := newRequest(t, url, io.NopCloser(strings.NewReader(body)))
			req.ContentLength = -1

			return client.Do(req)
		}},
		{"Do with ContentLength 0", func(t *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			t.Helper()

			// net/http reads 0 with a non-empty Body as unknown
			return client.Do(newRequest(t, url, io.NopCloser(strings.NewReader(body))))
		}},
		{"bytes.Buffer", func(_ *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			return client.Post(url, "text/plain", bytes.NewBufferString(body))
		}},
		{"bytes.Reader", func(_ *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			return client.Post(url, "text/plain", bytes.NewReader([]byte(body)))
		}},
		{"opaque body", func(_ *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			// the way nuclei passes a raw request's body
			return client.DoRaw(http.MethodPost, url, "", nil, io.NopCloser(strings.NewReader(body)))
		}},
		{"caller header", func(_ *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			// raw header maps are not canonicalized, and the body type hides its length
			headers := map[string][]string{"content-length": {length}}

			return client.DoRaw(http.MethodPost, url, "", headers, io.NopCloser(strings.NewReader(body)))
		}},
		{"Dor with caller header", func(t *testing.T, client *PipelineClient, url string) (*http.Response, error) {
			t.Helper()

			req := newRetryableRequest(t, url, body)
			req.Header["Content-length"] = []string{length}

			return client.Dor(req)
		}},
	}
}

// A request with a body carries one Content-Length that matches the body, so
// the server frames the next pipelined request at its request line. A
// Content-Length the caller sets is sent once, as given.
func TestPipelineClientBodyContentLength(t *testing.T) {
	t.Parallel()

	const body = "a=1"

	length := strconv.Itoa(len(body))

	for _, test := range bodyCases(body) {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client, url, got := wireClient(t)

			for attempt := 1; attempt <= 2; attempt++ {
				resp, err := test.send(t, client, url)
				checkOK(t, resp, err)

				req := <-got

				wantFraming := req.line == "POST / HTTP/1.1" && len(req.length) == 1 && req.length[0] == length
				if !wantFraming || req.body != body {
					t.Fatalf("request %d framed as %q, Content-Length %q, body %q; want POST / HTTP/1.1, [%s], %q",
						attempt, req.line, req.length, req.body, length, body)
				}
			}
		})
	}
}

// A request without a body, or with an empty one, carries no Content-Length.
func TestPipelineClientEmptyBodyHasNoContentLength(t *testing.T) {
	t.Parallel()

	client, url, got := wireClient(t)

	for attempt := 1; attempt <= 2; attempt++ {
		resp, err := client.Post(url, "text/plain", strings.NewReader(""))
		checkOK(t, resp, err)

		if req := <-got; len(req.length) != 0 {
			t.Fatalf("request %d sent Content-Length %q for an empty body", attempt, req.length)
		}
	}
}

var errBodyRead = errors.New("body read failed")

// failingReader is a body of unknown length whose read fails.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errBodyRead }

// A body that cannot be read for its length fails the call, and the server
// sees none of it: the next request on the connection is framed as usual.
func TestPipelineClientBodyReadError(t *testing.T) {
	t.Parallel()

	client, url, got := wireClient(t)

	resp, err := client.DoRaw(http.MethodPost, url, "", nil, io.NopCloser(failingReader{}))
	if resp != nil {
		_ = resp.Body.Close()

		t.Fatalf("DoRaw returned a response %v with the error", resp.Status)
	}

	if !errors.Is(err, errBodyRead) {
		t.Fatalf("DoRaw error = %v, want %v", err, errBodyRead)
	}

	resp, err = client.Post(url, "text/plain", strings.NewReader("a=1"))
	checkOK(t, resp, err)

	if req := <-got; req.line != "POST / HTTP/1.1" || req.body != "a=1" {
		t.Fatalf("server read %q with body %q first, want the second request", req.line, req.body)
	}
}

// A caller that frames the body itself with Transfer-Encoding gets it sent as
// given: a Content-Length next to it would give the message two lengths.
func TestPipelineClientKeepsCallerTransferEncoding(t *testing.T) {
	t.Parallel()

	const chunked = "3\r\na=1\r\n0\r\n\r\n"

	listener := listen(t)
	got := make(chan []byte, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		var raw []byte

		buf := make([]byte, 4096)

		for !bytes.HasSuffix(raw, []byte(chunked)) {
			n, err := conn.Read(buf)
			if err != nil {
				break
			}

			raw = append(raw, buf[:n]...)
		}

		got <- raw

		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	}()

	opts := DefaultPipelineOptions
	opts.Host = listener.Addr().String()
	opts.Timeout = 5 * time.Second
	client := NewPipelineClient(opts)

	headers := map[string][]string{"transfer-encoding": {"chunked"}}
	body := io.NopCloser(strings.NewReader(chunked))
	resp, err := client.DoRaw(http.MethodPost, "http://"+opts.Host+"/", "", headers, body)
	checkOK(t, resp, err)

	raw := <-got
	if bytes.Contains(bytes.ToLower(raw), []byte("content-length")) {
		t.Fatalf("server read a Content-Length next to Transfer-Encoding: %q", raw)
	}

	if !bytes.HasSuffix(raw, []byte(chunked)) {
		t.Fatalf("server read %q, want it to end with the caller's chunked body %q", raw, chunked)
	}
}

// listen returns a loopback listener closed when the test ends.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}
