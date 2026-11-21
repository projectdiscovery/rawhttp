package rawhttp

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/projectdiscovery/rawhttp/clientpipeline"
	retryablehttp "github.com/projectdiscovery/retryablehttp-go"
	urlutil "github.com/projectdiscovery/utils/url"
)

// PipelineClient is a client for making pipelined http requests
type PipelineClient struct {
	// connections are pooled per scheme; the request URL picks one
	plainClient *clientpipeline.PipelineClient
	tlsClient   *clientpipeline.PipelineClient
	options     PipelineOptions
}

// NewPipelineClient creates a new pipelined http request client
func NewPipelineClient(options PipelineOptions) *PipelineClient {
	return &PipelineClient{
		plainClient: newSchemeClient(options, false),
		tlsClient:   newSchemeClient(options, true),
		options:     options,
	}
}

// newSchemeClient returns the pipelined connections to options.Host for one
// scheme. It dials nothing until the first request.
func newSchemeClient(options PipelineOptions, isTLS bool) *clientpipeline.PipelineClient {
	client := &clientpipeline.PipelineClient{
		Dial:               options.Dialer,
		Addr:               options.Host,
		MaxConns:           options.MaxConnections,
		MaxPendingRequests: options.MaxPendingRequests,
		ReadTimeout:        options.Timeout,
		// also bounds the TLS handshake, which otherwise has no deadline
		// when a request larger than the write buffer triggers it
		WriteTimeout: options.Timeout,
		IsTLS:        isTLS,
	}
	if isTLS {
		// same policy as Client: targets are arbitrary hosts, so skip verification
		client.TLSConfig = &tls.Config{InsecureSkipVerify: true, Renegotiation: tls.RenegotiateOnceAsClient}
	}
	return client
}

// Head makes a HEAD request to a given URL
func (c *PipelineClient) Head(url string) (*http.Response, error) {
	return c.DoRaw("HEAD", url, "", nil, nil)
}

// Get makes a GET request to a given URL
func (c *PipelineClient) Get(url string) (*http.Response, error) {
	return c.DoRaw("GET", url, "", nil, nil)
}

// Post makes a POST request to a given URL
func (c *PipelineClient) Post(url string, mimetype string, body io.Reader) (*http.Response, error) {
	headers := make(map[string][]string)
	headers["Content-Type"] = []string{mimetype}
	return c.DoRaw("POST", url, "", headers, body)
}

// Do sends a http request and returns a response
func (c *PipelineClient) Do(req *http.Request) (*http.Response, error) {
	method := req.Method
	headers := req.Header
	url := req.URL.String()
	body := req.Body

	return c.do(method, url, "", headers, body, req.ContentLength, c.options)
}

// Dor sends a retryablehttp request and returns a response
func (c *PipelineClient) Dor(req *retryablehttp.Request) (*http.Response, error) {
	method := req.Method
	headers := req.Header
	url := req.URL.String()
	body := req.Body

	return c.do(method, url, "", headers, body, req.ContentLength, c.options)
}

// DoRaw does a raw request with some configuration
func (c *PipelineClient) DoRaw(method, url, uripath string, headers map[string][]string, body io.Reader) (*http.Response, error) {
	return c.do(method, url, uripath, headers, body, -1, c.options)
}

// DoRawWithOptions performs a raw request with additional options
func (c *PipelineClient) DoRawWithOptions(method, url, uripath string, headers map[string][]string, body io.Reader, options PipelineOptions) (*http.Response, error) {
	return c.do(method, url, uripath, headers, body, -1, options)
}

// do sends the request. contentLength is the body length when the caller
// knows it, and 0 or less otherwise, as in http.Request.ContentLength.
func (c *PipelineClient) do(
	method, url, uripath string,
	headers map[string][]string,
	body io.Reader,
	contentLength int64,
	options PipelineOptions,
) (*http.Response, error) {
	if headers == nil {
		headers = make(map[string][]string)
	}
	u, err := urlutil.ParseURL(url, true)
	if err != nil {
		return nil, err
	}

	req := clientpipeline.ToRequest(method, requestPath(u, uripath), nil, headers, body)

	err = setContentLength(req, headers, contentLength)
	if err != nil {
		return nil, err
	}

	// a caller-supplied Host wins; RFC 9110 asks for Host as the first field
	if options.AutomaticHostHeader && u.Host != "" && !hasHeader(headers, "Host") {
		req.Headers = append([]clientpipeline.Header{{Key: "Host", Value: u.Host}}, req.Headers...)
	}

	client := c.plainClient
	if u.Scheme == "https" {
		client = c.tlsClient
	}

	var resp clientpipeline.Response
	err = client.Do(req, &resp)

	// response => net/http response
	r := http.Response{
		StatusCode:    resp.Status.Code,
		ContentLength: resp.ContentLength(),
		Header:        make(http.Header),
	}

	for _, header := range resp.Headers {
		r.Header.Set(header.Key, header.Value)
	}

	r.Body = io.NopCloser(resp.Body)

	return &r, err
}

// requestPath returns the request target for parsed, or uripath when the
// caller supplies one.
func requestPath(parsed *urlutil.URL, uripath string) string {
	if uripath != "" {
		return uripath
	}

	path := parsed.Path
	if path == "" {
		path = "/"
	}

	if !parsed.Params.IsEmpty() {
		path += "?" + parsed.Params.Encode()
	}

	return path
}

// setContentLength gives req a Content-Length, because a body the server
// cannot frame is read as the start of the next request on the connection. A
// caller that sets Content-Length or Transfer-Encoding frames the body itself,
// and an empty body needs no length.
func setContentLength(req *clientpipeline.Request, headers map[string][]string, contentLength int64) error {
	if req.Body == nil || hasHeader(headers, "Content-Length") || hasHeader(headers, "Transfer-Encoding") {
		return nil
	}

	length, body, err := bodyLength(req.Body, contentLength)
	if err != nil {
		return err
	}

	req.Body = body

	if length > 0 {
		req.Headers = append(req.Headers, clientpipeline.Header{Key: "Content-Length", Value: strconv.FormatInt(length, 10)})
	}

	return nil
}

// bodyLength returns the length of body and a reader that yields it. It trusts
// contentLength when it is above 0, then the length of a body type that
// reports one, and otherwise reads the body into memory to count it.
func bodyLength(body io.Reader, contentLength int64) (int64, io.Reader, error) {
	if contentLength > 0 {
		return contentLength, body, nil
	}

	switch typed := body.(type) {
	case *bytes.Buffer:
		return int64(typed.Len()), body, nil
	case *bytes.Reader:
		return int64(typed.Len()), body, nil
	case *strings.Reader:
		return int64(typed.Len()), body, nil
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return 0, nil, fmt.Errorf("read request body: %w", err)
	}

	return int64(len(data)), bytes.NewReader(data), nil
}

// hasHeader reports whether headers contains key, ignoring case as raw
// header maps are not canonicalized.
func hasHeader(headers map[string][]string, key string) bool {
	for k := range headers {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}
