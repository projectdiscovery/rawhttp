package rawhttp

import "time"

type Options struct {
	Timeout                time.Duration
	FollowRedirects        bool
	MaxRedirects           int
	AutomaticHostHeader    bool
	AutomaticContentLength bool
}

var DefaultOptions = Options{
	Timeout:                30 * time.Second,
	FollowRedirects:        true,
	MaxRedirects:           10,
	AutomaticHostHeader:    true,
	AutomaticContentLength: true,
}
