package rawhttp

import (
	"time"

	"github.com/projectdiscovery/rawhttp/client"
)

// Options contains configuration options for rawhttp client
type Options struct {
	Timeout                time.Duration
	FollowRedirects        bool
	MaxRedirects           int
	AutomaticHostHeader    bool
	AutomaticContentLength bool
	CustomHeaders          client.Headers
	CustomRawBytes         []byte
}

// DefaultOptions is the default configuration options for the client
var DefaultOptions = Options{
	Timeout:                30 * time.Second,
	FollowRedirects:        true,
	MaxRedirects:           10,
	AutomaticHostHeader:    true,
	AutomaticContentLength: true,
}
