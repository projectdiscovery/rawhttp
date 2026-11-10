package rawhttp

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func rawSrv(t *testing.T, responses ...string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		i := 0
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			resp := responses[i%len(responses)]
			i++
			go func(c net.Conn, resp string) {
				defer func() { _ = c.Close() }()
				br := bufio.NewReader(c)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if line == "\r\n" || line == "\n" {
						break
					}
				}
				_, _ = c.Write([]byte(resp))
				time.Sleep(30 * time.Millisecond)
			}(c, resp)
		}
	}()
	return "http://" + ln.Addr().String()
}

func cli(t *testing.T) (*Client, *Options) {
	t.Helper()
	o := *DefaultOptions
	o.Timeout = 5 * time.Second
	return &Client{dialer: new(dialer), Options: &o}, &o
}

func gzm(s string) string {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

// Field names are case insensitive (RFC 9110 s5.1). A conformant client must
// gzip-decode and expose Content-Encoding regardless of the case the server
// chose. These assertions encode the CORRECT behaviour; they fail on the bug.
func TestCE(t *testing.T) {
	const marker = "SENSITIVE-MARKER-9f3a"
	packed := gzm(marker)
	for _, name := range []string{"Content-Encoding", "content-encoding"} {
		t.Run(name, func(t *testing.T) {
			resp := "HTTP/1.1 200 OK\r\n" + name + ": gzip\r\nContent-Length: " +
				strconv.Itoa(len(packed)) + "\r\n\r\n" + packed
			url := rawSrv(t, resp)
			c, o := cli(t)
			r, err := c.DoRawWithOptions("GET", url, "/x", nil, nil, o)
			if err != nil {
				t.Fatalf("DoRawWithOptions: %v", err)
			}
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			t.Logf("RESULT field=%-16q bodylen=%d Get(Content-Encoding)=%q", name, len(body), r.Header.Get("Content-Encoding"))
			if string(body) != marker {
				t.Errorf("field %q: body=%q (len %d), want decompressed %q", name, truncHP(body), len(body), marker)
			}
			if got := r.Header.Get("Content-Encoding"); got != "gzip" {
				t.Errorf("field %q: resp.Header.Get(\"Content-Encoding\")=%q, want \"gzip\" (http.Header canonical-key contract)", name, got)
			}
		})
	}
}

// A conformant client follows a 302 regardless of the case of "Location".
func TestLoc(t *testing.T) {
	for _, name := range []string{"Location", "location"} {
		t.Run(name, func(t *testing.T) {
			redirect := "HTTP/1.1 302 Found\r\n" + name + ": /landing\r\nContent-Length: 0\r\n\r\n"
			landing := "HTTP/1.1 200 OK\r\nContent-Length: 7\r\n\r\nlanded!"
			url := rawSrv(t, redirect, landing)
			c, o := cli(t)
			r, err := c.DoRawWithOptions("GET", url, "/start", nil, nil, o)
			if err != nil {
				t.Errorf("field %q: DoRawWithOptions returned error %v; a 302 must not error", name, err)
				return
			}
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			t.Logf("RESULT field=%-10q final_status=%d body=%q", name, r.StatusCode, body)
			if r.StatusCode != 200 || string(body) != "landed!" {
				t.Errorf("field %q: landed on status=%d body=%q; redirect not followed", name, r.StatusCode, body)
			}
		})
	}
}

func truncHP(b []byte) string {
	if len(b) > 24 {
		return string(b[:24]) + "..."
	}
	return string(b)
}
