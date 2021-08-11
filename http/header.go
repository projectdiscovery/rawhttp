// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package http

import (
	"io"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"time"
)

type HeaderOptions struct {
	Separator  string
	Terminator string
}

var DefaultHeaderOptions HeaderOptions = HeaderOptions{
	Separator:  ": ",
	Terminator: "\r\n",
}

// A Header represents the key-value pairs in an HTTP header.
//
// The keys should be in canonical form, as returned by
// CanonicalHeaderKey.
type Header map[string][]string

// Add adds the key, value pair to the header.
// It appends to any existing values associated with key.
// The key is case insensitive; it is canonicalized by
// CanonicalHeaderKey.
func (h Header) Add(key, value string) {
	textproto.MIMEHeader(h).Add(key, value)
}

// Set sets the header entries associated with key to the
// single element value. It replaces any existing values
// associated with key. The key is case insensitive; it is
// canonicalized by textproto.CanonicalMIMEHeaderKey.
// To use non-canonical keys, assign to the map directly.
func (h Header) Set(key, value string) {
	textproto.MIMEHeader(h).Set(key, value)
}

// Get gets the first value associated with the given key. If
// there are no values associated with the key, Get returns "".
// It is case insensitive; textproto.CanonicalMIMEHeaderKey is
// used to canonicalize the provided key. To use non-canonical keys,
// access the map directly.
func (h Header) Get(key string) string {
	return textproto.MIMEHeader(h).Get(key)
}

// Values returns all values associated with the given key.
// It is case insensitive; textproto.CanonicalMIMEHeaderKey is
// used to canonicalize the provided key. To use non-canonical
// keys, access the map directly.
// The returned slice is not a copy.
func (h Header) Values(key string) []string {
	return textproto.MIMEHeader(h).Values(key)
}

// get is like Get, but key must already be in CanonicalHeaderKey form.
func (h Header) get(key string) string {
	if v := h[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// has reports whether h has the provided key defined, even if it's
// set to 0-length slice.
func (h Header) has(key string) bool {
	_, ok := h[key]
	return ok
}

// Del deletes the values associated with key.
// The key is case insensitive; it is canonicalized by
// CanonicalHeaderKey.
func (h Header) Del(key string) {
	textproto.MIMEHeader(h).Del(key)
}

// Write writes a header in wire format.
func (h Header) Write(w io.Writer) error {
	return h.write(w, nil)
}

func (h Header) write(w io.Writer, trace *httptrace.ClientTrace) error {
	return h.writeSubset(w, nil, trace, DefaultHeaderOptions)
}

func (h Header) writeWithOptions(w io.Writer, trace *httptrace.ClientTrace, options HeaderOptions) error {
	return h.writeSubset(w, nil, trace, options)
}

// Clone returns a copy of h or nil if h is nil.
func (h Header) Clone() Header {
	if h == nil {
		return nil
	}

	// Find total number of values.
	nv := 0
	for _, vv := range h {
		nv += len(vv)
	}
	sv := make([]string, nv) // shared backing array for headers' values
	h2 := make(Header, len(h))
	for k, vv := range h {
		n := copy(sv, vv)
		h2[k] = sv[:n:n]
		sv = sv[n:]
	}
	return h2
}

var timeFormats = []string{
	TimeFormat,
	time.RFC850,
	time.ANSIC,
}

// ParseTime parses a time header (such as the Date: header),
// trying each of the three formats allowed by HTTP/1.1:
// TimeFormat, time.RFC850, and time.ANSIC.
func ParseTime(text string) (t time.Time, err error) {
	for _, layout := range timeFormats {
		t, err = time.Parse(layout, text)
		if err == nil {
			return
		}
	}
	return
}

// stringWriter implements WriteString on a Writer.
type stringWriter struct {
	w io.Writer
}

func (w stringWriter) WriteString(s string) (n int, err error) {
	return w.w.Write([]byte(s))
}

func (h Header) WriteSubset(w io.Writer, exclude map[string]bool) error {
	return h.writeSubset(w, exclude, nil, DefaultHeaderOptions)
}

func (h Header) WriteSubsetWithOptions(w io.Writer, exclude map[string]bool, options HeaderOptions) error {
	return h.writeSubset(w, exclude, nil, options)
}

func (h Header) writeSubset(w io.Writer, exclude map[string]bool, trace *httptrace.ClientTrace, options HeaderOptions) error {
	ws, ok := w.(io.StringWriter)
	if !ok {
		ws = stringWriter{w}
	}

	for k, v := range h {
		if _, err := ws.WriteString(k); err != nil {
			return err
		}
		if len(v) > 0 {
			if _, err := ws.WriteString(options.Separator); err != nil {
				return err
			}

			for _, s := range v {
				if _, err := ws.WriteString(s); err != nil {
					return err
				}
			}
		}

		if _, err := ws.WriteString(options.Terminator); err != nil {
			return err
		}

	}

	return nil
}

// CanonicalHeaderKey returns the canonical format of the
// header key s. The canonicalization converts the first
// letter and any letter following a hyphen to upper case;
// the rest are converted to lowercase. For example, the
// canonical key for "accept-encoding" is "Accept-Encoding".
// If s contains a space or invalid header field bytes, it is
// returned without modifications.
func CanonicalHeaderKey(s string) string { return textproto.CanonicalMIMEHeaderKey(s) }

// hasToken reports whether token appears with v, ASCII
// case-insensitive, with space or comma boundaries.
// token must be all lowercase.
// v may contain mixed cased.
func hasToken(v, token string) bool {
	if len(token) > len(v) || token == "" {
		return false
	}
	if v == token {
		return true
	}
	for sp := 0; sp <= len(v)-len(token); sp++ {
		// Check that first character is good.
		// The token is ASCII, so checking only a single byte
		// is sufficient. We skip this potential starting
		// position if both the first byte and its potential
		// ASCII uppercase equivalent (b|0x20) don't match.
		// False positives ('^' => '~') are caught by EqualFold.
		if b := v[sp]; b != token[0] && b|0x20 != token[0] {
			continue
		}
		// Check that start pos is on a valid token boundary.
		if sp > 0 && !isTokenBoundary(v[sp-1]) {
			continue
		}
		// Check that end pos is on a valid token boundary.
		if endPos := sp + len(token); endPos != len(v) && !isTokenBoundary(v[endPos]) {
			continue
		}
		if strings.EqualFold(v[sp:sp+len(token)], token) {
			return true
		}
	}
	return false
}

func isTokenBoundary(b byte) bool {
	return b == ' ' || b == ',' || b == '\t'
}
