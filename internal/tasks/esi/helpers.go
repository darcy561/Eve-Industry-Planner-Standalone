package esi

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// countingReader counts bytes read from an underlying reader
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// parseCacheSeconds extracts max-age seconds from Cache-Control or computes from Expires header
func parseCacheSeconds(resp *http.Response) int {
	cc := resp.Header.Get("Cache-Control")
	if cc != "" {
		parts := strings.Split(cc, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "max-age=") {
				v := strings.TrimPrefix(p, "max-age=")
				if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
					return secs
				}
			}
		}
	}
	if exp := resp.Header.Get("Expires"); exp != "" {
		if t, err := http.ParseTime(exp); err == nil {
			d := time.Until(t)
			if d > 0 {
				return int(d.Seconds())
			}
		}
	}
	return 0
}

