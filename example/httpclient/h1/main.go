package main

import (
	"log"
	"net/url"

	"github.com/projectdiscovery/rawhttp/crypto/tls"

	"github.com/projectdiscovery/rawhttp/net/http"
	"github.com/projectdiscovery/rawhttp/net/http/httputil"
)

func main() {
	target := "http://scanme.sh"
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	client := &http.Client{Transport: transport}

	log.Println("standard request")
	req, err := http.NewRequest(http.MethodGet, target+"/standard", nil)
	if err != nil {
		log.Fatal(err)
	}
	reqDump, respDump, err := sendAndDump(client, req)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("request:\n%s\nresponse:\n%s", string(reqDump), string(respDump))

	log.Println("request with invalid header:")
	req, err = http.NewRequest(http.MethodGet, target+"/invalid-header", nil)
	if err != nil {
		log.Fatal(err)
	}
	// add non-rfc header
	req.Unsafe = true
	req.Header["test test"] = []string{"test"}
	reqDump, respDump, err = sendAndDump(client, req)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("request:\n%s\nresponse:\n%s", string(reqDump), string(respDump))

	log.Println("request with unescaped path")
	req, err = http.NewRequest(http.MethodGet, target+"/?bar=;&baz=foobar&abc&xyz=&ikj=n;m \"'@", nil)
	if err != nil {
		log.Fatal(err)
	}
	// add non-rfc header
	req.Unsafe = true
	reqDump, respDump, err = sendAndDump(client, req)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("request:\n%s\nresponse:\n%s", string(reqDump), string(respDump))

	log.Println("request with proxy (burp/proxify must be listening on port 8080)")
	proxyURL, _ := url.Parse("http://127.0.0.1:8080")
	transport.Proxy = http.ProxyURL(proxyURL)
	req, err = http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		log.Fatal(err)
	}
	reqDump, respDump, err = sendAndDump(client, req)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("request:\n%s\nresponse:\n%s", string(reqDump), string(respDump))
}

func sendAndDump(client *http.Client, req *http.Request) ([]byte, []byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	reqDump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return nil, nil, err
	}
	if err != nil {
		return reqDump, nil, err
	}
	respDump, err := httputil.DumpResponse(resp, true)
	return reqDump, respDump, err
}
