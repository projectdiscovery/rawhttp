package httpclient

import (
	"fmt"
	"strings"

	"github.com/projectdiscovery/rawhttp/net/http"
	"github.com/projectdiscovery/rawhttp/net/http/httputil"
)

var DisableLogging = false

// SendAndDump sends http request with client and returns respose
// and dumps the request and response to stdout
// It is meant to be used for debugging purposes
func SendAndDump(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		// fmt.Printf("[Error]: failed to send request with client: %v\n", err)
		return resp, err
	}

	if !DisableLogging {
		fmt.Printf("%s\n", getMultiples('-', 30))
		reqDump, err := httputil.DumpRequestOut(req, true)
		if err != nil {
			fmt.Printf("[Error]:failed to dump request out: %v\n", err)
		} else {
			fmt.Printf("[+] Request:\n%s\n", reqDump)
		}
		respDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			fmt.Printf("[Error]:failed to dump response: %v\n", err)
		} else {
			fmt.Printf("[+] Response:\n%s\n", respDump)
		}
		fmt.Printf("\n\n")
	}
	return resp, err
}

func getMultiples(delim rune, count int) string {
	if count == 0 {
		return ""
	}
	return strings.Repeat(string(delim), count)
}
