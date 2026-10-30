package client

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A response can end right after its status line. ReadResponse must not wait
// for bytes that are not coming: headerless responses are what HTTP proxies
// send for CONNECT.
func TestReadResponseWithoutHeadersDoesNotBlock(t *testing.T) {
	server, conn := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = conn.Close() }()

	release := make(chan struct{})
	defer close(release)
	go func() {
		_, _ = server.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		<-release
	}()

	type result struct {
		resp *Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := NewClient(conn).ReadResponse(false)
		done <- result{resp, err}
	}()

	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Equal(t, 200, got.resp.Status.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("ReadResponse blocked on a response without headers")
	}
}

// Duplicate status lines are still skipped when they arrive in a separate
// write, without looking ahead for bytes that may never come.
func TestReadResponseDuplicateStatusLinesAcrossWrites(t *testing.T) {
	server, conn := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = conn.Close() }()

	go func() {
		_, _ = server.Write([]byte("HTTP/1.1 200 OK\r\n"))
		time.Sleep(10 * time.Millisecond)
		_, _ = server.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nhi"))
	}()

	resp, err := NewClient(conn).ReadResponse(false)
	require.NoError(t, err)
	require.Equal(t, 200, resp.Status.Code)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "hi", string(body))
}
