package rawhttp

import (
	"io"
	"net/http"

	retryablehttp "github.com/projectdiscovery/retryablehttp-go"
)

var DefaultClient = Client{
	dialer:  new(dialer),
	options: DefaultOptions,
}

func Get(url string) (*http.Response, error) {
	return DefaultClient.Get(url)
}

func Post(url string, mimetype string, r io.Reader) (*http.Response, error) {
	return DefaultClient.Post(url, mimetype, r)
}

func Do(req *http.Request) (*http.Response, error) {
	return DefaultClient.Do(req)
}
func Dor(req *retryablehttp.Request) (*http.Response, error) {
	return DefaultClient.Dor(req)
}

func DoRaw(method, url, uripath string, headers map[string][]string, body io.Reader) (*http.Response, error) {
	return DefaultClient.DoRaw(method, url, uripath, headers, body)
}
