package main

import (
	"log"

	"github.com/projectdiscovery/rawhttp/tls"

	"github.com/projectdiscovery/rawhttp/http"
)

func main() {
	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:8000", nil)
	req.Header["test test"] = []string{"test"}
	req.Unsafe = true
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(resp)
}
