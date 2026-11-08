package clientpipeline

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPipelineClientReconnectsAfterIdle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer ts.Close()

	client := &PipelineClient{
		Addr:                strings.TrimPrefix(ts.URL, "http://"),
		MaxConns:            1,
		MaxIdleConnDuration: 50 * time.Millisecond,
		ReadTimeout:         time.Second,
		WriteTimeout:        time.Second,
	}
	do := func() error {
		done := make(chan error, 1)
		go func() {
			req := ToRequest("GET", "/", nil, nil, nil)
			req.Headers = append(req.Headers, Header{Key: "Host", Value: client.Addr})
			var resp Response
			done <- client.Do(req, &resp)
		}()
		select {
		case err := <-done:
			return err
		case <-time.After(3 * time.Second):
			return fmt.Errorf("request hung")
		}
	}

	require.NoError(t, do())
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, do(), "a request after the idle connection closed must reconnect")
}
