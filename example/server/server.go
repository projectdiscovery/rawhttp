package main

import (
	"fmt"
	"net/http"

	"github.com/projectdiscovery/gologger"
)

func headers(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for name, headers := range req.Header {
		for _, h := range headers {
			_, _ = fmt.Fprintf(w, "%v: %v\n", name, h)
		}
	}
}

func main() {
	http.HandleFunc("/headers", headers)
	if err := http.ListenAndServe(":10000", nil); err != nil {
		gologger.Fatal().Msgf("Could not listen and serve: %s\n", err)
	}
}
