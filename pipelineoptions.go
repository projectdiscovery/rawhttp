package rawhttp

import "time"

type PipelineOptions struct {
	Host                string
	Timeout             time.Duration
	MaxConnections      int
	MaxPendingRequests  int
	AutomaticHostHeader bool
}

var DefaultPipelineOptions = PipelineOptions{
	Timeout:             30 * time.Second,
	MaxConnections:      5,
	AutomaticHostHeader: true,
}
