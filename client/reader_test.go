package client

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type statusTest struct {
	name       string
	statusLine string
	result     int
	err        bool
}

func TestStatusCode(t *testing.T) {
	tests := []statusTest{
		{"redirect 301", "301\r\n", 301, false},
		{"200 ok", "200 ", 200, false},
		{"300 redirect", "300 ", 300, false},
		{"0 unknown", "0 ", 0, false},
		{"long number", "4578 ", 4578, false},
		{"invalid string", "aaa ", 0, true},
		{"number with status text", "1234 unknown", 1234, false},
	}
	for _, test := range tests {
		r := reader{bufio.NewReader(strings.NewReader(test.statusLine))}
		result, err := r.ReadStatusCode()
		hasError := err != nil
		require.Equal(t, test.err, hasError, err)
		require.Equal(t, test.result, result)
	}
}
