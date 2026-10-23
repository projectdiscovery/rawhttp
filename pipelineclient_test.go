package rawhttp

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
