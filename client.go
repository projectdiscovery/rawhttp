package rawhttp

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	stdurl "net/url"
	"strings"
	"time"

	retryablehttp "github.com/projectdiscovery/retryablehttp-go"
)

type Client struct {
	dialer  Dialer
	options Options
}

func AutomaticHostHeader(enable bool) {
	DefaultClient.options.AutomaticHostHeader = enable
}

func AutomaticContentLength(enable bool) {
	DefaultClient.options.AutomaticContentLength = enable
}

func NewClient(options Options) *Client {
	client := &Client{
		dialer:  new(dialer),
		options: options,
	}
	return client
}

func (c *Client) Head(url string) (*http.Response, error) {
	return c.DoRaw("HEAD", url, "", nil, nil)
}

func (c *Client) Get(url string) (*http.Response, error) {
	return c.DoRaw("GET", url, "", nil, nil)
}

func (c *Client) Post(url string, mimetype string, body io.Reader) (*http.Response, error) {
	headers := make(map[string][]string)
	headers["Content-Type"] = []string{mimetype}
	return c.DoRaw("POST", url, "", headers, body)
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	method := req.Method
	headers := req.Header
	url := req.URL.String()
	body := req.Body

	return c.DoRaw(method, url, "", headers, body)
}

func (c *Client) Dor(req *retryablehttp.Request) (*http.Response, error) {
	method := req.Method
	headers := req.Header
	url := req.RequestURI
	body := req.Body

	return c.DoRaw(method, url, "", headers, body)
}

func (c *Client) DoRaw(method, url, uripath string, headers map[string][]string, body io.Reader) (*http.Response, error) {
	redirectstatus := &RedirectStatus{
		FollowRedirects: true,
		MaxRedirects:    c.options.MaxRedirects,
	}
	return c.do(method, url, uripath, headers, body, redirectstatus)
}

func (c *Client) do(method, url, uripath string, headers map[string][]string, body io.Reader, redirectstatus *RedirectStatus) (*http.Response, error) {
	if headers == nil {
		headers = make(map[string][]string)
	}
	u, err := stdurl.ParseRequestURI(url)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if c.options.AutomaticHostHeader {
		headers["Host"] = []string{host}
	}

	protocol := strings.ToLower(url[:strings.IndexByte(url, ':')])
	if !strings.Contains(host, ":") {
		if protocol == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	// standard path
	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	// override if custom one is specified
	if uripath != "" {
		path = uripath
	}
	conn, err := c.dialer.Dial(protocol, host)
	if err != nil {
		return nil, err
	}

	req := toRequest(method, path, nil, headers, body)
	req.AutomaticContentLength = c.options.AutomaticContentLength
	req.AutomaticHost = c.options.AutomaticHostHeader

	// set timeout if any
	if c.options.Timeout > 0 {
		conn.SetDeadline(time.Now().Add(c.options.Timeout))
	}

	if err := conn.WriteRequest(req); err != nil {
		return nil, err
	}
	resp, err := conn.ReadResponse()
	if err != nil {
		return nil, err
	}

	r, err := toHttpResponse(conn, resp)
	if err != nil {
		return nil, err
	}

	if resp.Status.IsRedirect() && redirectstatus.FollowRedirects && redirectstatus.Current <= redirectstatus.MaxRedirects {
		// consume the response body
		_, err := io.Copy(ioutil.Discard, r.Body)
		if err := firstErr(err, r.Body.Close()); err != nil {
			return nil, err
		}
		loc := headerValue(r.Header, "Location")
		if strings.HasPrefix(loc, "/") {
			loc = fmt.Sprintf("%s://%s%s", protocol, host, loc)
		}
		redirectstatus.Current++
		return c.do(method, loc, uripath, headers, body, redirectstatus)
	}

	return r, err
}

type RedirectStatus struct {
	FollowRedirects bool
	MaxRedirects    int
	Current         int
}
