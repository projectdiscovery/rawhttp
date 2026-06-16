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
