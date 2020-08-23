package rawhttp

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/projectdiscovery/rawhttp/client"
)

type StatusError struct {
	client.Status
}

func (s *StatusError) Error() string {
	return s.Status.String()
}

type readCloser struct {
	io.Reader
	io.Closer
}

func toRequest(method string, path string, query []string, headers map[string][]string, body io.Reader) *client.Request {
	return &client.Request{
		Method:  method,
		Path:    path,
		Query:   query,
		Version: client.HTTP_1_1,
		Headers: toHeaders(headers),
		Body:    body,
	}
}

func fromResponse(resp *client.Response) (client.Version, client.Status, map[string][]string, io.Reader) {
	body := resp.Body
	headers := fromHeaders(resp.Headers)
	return resp.Version, resp.Status, headers, body
}

func toHttpResponse(conn Conn, resp *client.Response) (*http.Response, error) {
	rheaders := fromHeaders(resp.Headers)
	r := http.Response{
		Status:        resp.Status.String(),
		StatusCode:    resp.Status.Code,
		Header:        rheaders,
		ContentLength: resp.ContentLength(),
	}

	var err error
	rbody := resp.Body
	if headerValue(rheaders, "Content-Encoding") == "gzip" {
		rbody, err = gzip.NewReader(rbody)
		if err != nil {
			return nil, err
		}
	}
	rc := &readCloser{rbody, conn}

	r.Body = rc

	return &r, nil
}

func toHeaders(h map[string][]string) []client.Header {
	var r []client.Header
	for k, v := range h {
		for _, v := range v {
			r = append(r, client.Header{Key: k, Value: v})
		}
	}
	return r
}

func fromHeaders(h []client.Header) map[string][]string {
	if h == nil {
		return nil
	}
	var r = make(map[string][]string)
	for _, hh := range h {
		r[hh.Key] = append(r[hh.Key], hh.Value)
	}
	return r
}

func headerValue(headers map[string][]string, key string) string {
	return strings.Join(headers[key], " ")
}

func firstErr(err1, err2 error) error {
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return nil
}
