package main

import (
	"flag"
	"fmt"
	"net/http/httputil"

	"github.com/projectdiscovery/rawhttp"
)

var (
	url   string
	short bool
)

func main() {
	flag.StringVar(&url, "url", "https://scanme.sh", "URL to fetch")
	flag.BoolVar(&short, "short", false, "Skip printing http response body")
	flag.Parse()

	client := rawhttp.NewClient(rawhttp.DefaultOptions)
	resp, err := client.Get(url)
	if err != nil {
		panic(err)
	}
	bin, err := httputil.DumpResponse(resp, !short)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(bin))
}
