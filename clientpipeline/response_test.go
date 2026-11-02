package clientpipeline

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type pipelinedExchange struct {
	method string
	path   string
	raw    string // the response the server writes for this request
	code   int
	body   string
	length int64 // the Content-Length the response reports, -1 if none
}

// servePipelined accepts one connection, reads one request per exchange and
// only then writes all responses in a single write, in the order the requests
// arrived, so the responses sit back to back in the client read buffer.
func servePipelined(t *testing.T, exchanges []pipelinedExchange) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	byPath := make(map[string]string, len(exchanges))
	for _, e := range exchanges {
		byPath[e.path] = e.raw
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		br := bufio.NewReader(conn)
		var out strings.Builder
		for range exchanges {
			req, err := http.ReadRequest(br)
			if err != nil {
				return
			}
			out.WriteString(byPath[req.URL.Path])
		}
		if _, err := io.WriteString(conn, out.String()); err != nil {
			return
		}
		// keep the connection open: a client that reads past a response
		// waits here instead of seeing EOF
		_, _ = io.Copy(io.Discard, conn)
	}()
	return ln.Addr().String()
}

func runPipelined(t *testing.T, exchanges []pipelinedExchange) {
	t.Helper()
	addr := servePipelined(t, exchanges)
	c := &PipelineClient{
		Addr:        addr,
		MaxConns:    1,
		ReadTimeout: 5 * time.Second,
	}

	type result struct {
		resp Response
		body string
		err  error
	}
	results := make([]result, len(exchanges))
	var wg sync.WaitGroup
	for i, e := range exchanges {
		wg.Add(1)
		go func(i int, e pipelinedExchange) {
			defer wg.Done()
			req := ToRequest(e.method, e.path, nil, map[string][]string{"Host": {addr}}, nil)
			res := &results[i]
			if res.err = c.Do(req, &res.resp); res.err != nil {
				return
			}
			b, err := io.ReadAll(res.resp.Body)
			res.body, res.err = string(b), err
		}(i, e)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipelined responses did not complete: a bodyless response is read until the deadline")
	}

	for i, e := range exchanges {
		res := results[i]
		name := fmt.Sprintf("%s %s", e.method, e.path)
		if res.err != nil {
			t.Errorf("%s: %v", name, res.err)
			continue
		}
		if res.resp.Status.Code != e.code || res.body != e.body {
			t.Errorf("%s = %d %q, want %d %q", name, res.resp.Status.Code, res.body, e.code, e.body)
		}
		if got := res.resp.ContentLength(); got != e.length {
			t.Errorf("%s: ContentLength() = %d, want the reported %d", name, got, e.length)
		}
	}
}

func TestPipelineBodylessStatus(t *testing.T) {
	runPipelined(t, []pipelinedExchange{
		{method: "GET", path: "/no-content", raw: "HTTP/1.1 204 No Content\r\n\r\n", code: 204, length: -1},
		{method: "GET", path: "/one", raw: "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nfirst", code: 200, body: "first", length: 5},
		// a 304 reports the length of the representation it validates
		{method: "GET", path: "/not-modified", raw: "HTTP/1.1 304 Not Modified\r\nContent-Length: 42\r\n\r\n", code: 304, length: 42},
		{method: "GET", path: "/two", raw: "HTTP/1.1 200 OK\r\nContent-Length: 6\r\n\r\nsecond", code: 200, body: "second", length: 6},
	})
}

func TestPipelineHeadResponse(t *testing.T) {
	runPipelined(t, []pipelinedExchange{
		{method: "HEAD", path: "/head", raw: "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\n", code: 200, length: 5},
		{method: "GET", path: "/get", raw: "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello", code: 200, body: "hello", length: 5},
	})
}
