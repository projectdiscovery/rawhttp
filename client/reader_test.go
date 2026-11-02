package client

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type statusTest struct {
	name       string
	statusLine string
	result     int
	err        bool
}

type versionTest struct {
	name    string
	version string
	major   int
	minor   int
	err     bool
}

func TestStatusCode(t *testing.T) {
	tests := []statusTest{
		{"redirect 301", "301\r\n", 301, false},
		{"200 ok", "200 ", 200, false},
		{"300 redirect", "300 ", 300, false},
		{"0 unknown", "0 ", 0, false},
		{"long number", "4578 ", 4578, false},
		{"invalid string", "aaa ", 0, true},
		{"number with status text", "1234 unknown", 1234, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := reader{bufio.NewReader(strings.NewReader(test.statusLine))}
			result, err := r.ReadStatusCode()
			hasError := err != nil
			require.Equal(t, test.err, hasError, err)
			require.Equal(t, test.result, result)
		})
	}
}

func TestReadVersion(t *testing.T) {
	tests := []versionTest{
		{"HTTP/0.9", "HTTP/0.9 OK", 0, 9, false},
		{"HTTP/1.0", "HTTP/1.0 OK", 1, 0, false},
		{"HTTP/1.1", "HTTP/1.1 OK", 1, 1, false},
		{"HTTP/2", "HTTP/2 OK", 2, 0, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := reader{bufio.NewReader(strings.NewReader(test.version))}
			result, err := r.ReadVersion()
			hasError := err != nil
			require.Equal(t, test.err, hasError, err)
			require.Equal(t, strings.TrimSuffix(test.version, " OK"), result.String())
			require.Equal(t, test.major, result.Major)
			require.Equal(t, test.minor, result.Minor)
		})
	}
}

// readWriterStub wraps a strings.Reader to satisfy io.ReadWriter (writes are no-ops).
type readWriterStub struct{ *strings.Reader }

func (readWriterStub) Write(p []byte) (int, error) { return len(p), nil }

func TestReadResponseDuplicateStatusLines(t *testing.T) {
	// Some non-compliant embedded devices (e.g. Grandstream HT801) emit the
	// HTTP status line twice before the headers. rawhttp must not error on this.
	raw := "HTTP/1.0 200 OK\r\nHTTP/1.0 200 OK\r\nContent-Type: text/html\r\nContent-Length: 5\r\n\r\nhello"
	c := NewClient(readWriterStub{strings.NewReader(raw)})
	resp, err := c.ReadResponse(false)
	require.NoError(t, err, "ReadResponse must not fail on duplicate status lines")
	require.Equal(t, 200, resp.Status.Code)
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "hello", string(body))
}

func TestReadResponseTripleStatusLines(t *testing.T) {
	// Also handle three consecutive status lines.
	raw := "HTTP/1.0 200 OK\r\nHTTP/1.0 200 OK\r\nHTTP/1.0 200 OK\r\nX-Custom: test\r\n\r\n"
	c := NewClient(readWriterStub{strings.NewReader(raw)})
	resp, err := c.ReadResponse(false)
	require.NoError(t, err, "ReadResponse must not fail on triple status lines")
	require.Equal(t, 200, resp.Status.Code)
	require.Equal(t, "test", resp.Headers[0].Value)
}

// TestReadResponseBodyless reads keep-alive responses back to back from one
// stream. A response that cannot carry a body must read none, so the next
// response starts at its status line.
func TestReadResponseBodyless(t *testing.T) {
	tests := []struct {
		name   string
		req    *Request
		raw    string
		code   int
		length int64
	}{
		{name: "204 without length", req: &Request{Method: "GET", Body: strings.NewReader("")}, raw: "HTTP/1.1 204 No Content\r\n\r\n", code: 204, length: -1},
		{name: "304 with length", req: &Request{Method: "GET", Body: strings.NewReader("")}, raw: "HTTP/1.1 304 Not Modified\r\nContent-Length: 42\r\n\r\n", code: 304, length: 42},
		{name: "1xx", req: &Request{Method: "GET", Body: strings.NewReader("")}, raw: "HTTP/1.1 102 Processing\r\nContent-Length: 3\r\n\r\n", code: 102, length: 3},
		{name: "HEAD with length", req: &Request{Method: "HEAD", Body: strings.NewReader("")}, raw: "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\n", code: 200, length: 5},
		{name: "raw HEAD with length", req: &Request{RawBytes: []byte("HEAD / HTTP/1.1\r\n\r\n")}, raw: "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\n", code: 200, length: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// requests carry an empty body: the writer only returns to the
			// request line phase after a body is written
			next := "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello"
			c := NewClient(readWriterStub{strings.NewReader(tt.raw + next)})

			require.NoError(t, c.WriteRequest(tt.req))
			resp, err := c.ReadResponse(false)
			require.NoError(t, err)
			require.Equal(t, tt.code, resp.Status.Code)
			require.Equal(t, tt.length, resp.ContentLength(), "the reported Content-Length must be kept")
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Empty(t, string(body))

			require.NoError(t, c.WriteRequest(&Request{Method: "GET", Body: strings.NewReader("")}))
			resp, err = c.ReadResponse(false)
			require.NoError(t, err)
			require.Equal(t, 200, resp.Status.Code)
			body, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, "hello", string(body))
		})
	}
}
