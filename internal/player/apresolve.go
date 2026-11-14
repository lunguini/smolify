package player

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// go-librespot dials access points in apresolve's order (or 443 first with
// PreferFirewallFriendlyPorts). Networks that inspect 443 drop the AP's
// non-TLS handshake while 80 passes, so we reorder apresolve's answer to
// try 80, then 443, then 4070. The daemon's HTTP client has no transport of
// its own, so wrapping http.DefaultTransport is the only hook.
// TODO: drop this file once https://github.com/devgianlu/go-librespot/pull/437
// is released: set daemon.Config.AccesspointPorts to []int{80, 443} instead.
var preferPort80Once sync.Once

func preferPort80() {
	preferPort80Once.Do(func() {
		http.DefaultTransport = apresolveTransport{http.DefaultTransport}
	})
}

type apresolveTransport struct{ base http.RoundTripper }

func (t apresolveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || req.URL.Host != "apresolve.spotify.com" || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	body = reorderAccesspoints(body)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return resp, nil
}

// reorderAccesspoints sorts the "accesspoint" list by port (80, 443, rest),
// keeping everything else; a body it cannot parse passes through unchanged.
func reorderAccesspoints(body []byte) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	var aps []string
	if json.Unmarshal(m["accesspoint"], &aps) != nil {
		return body
	}
	rank := func(addr string) int {
		switch {
		case strings.HasSuffix(addr, ":80"):
			return 0
		case strings.HasSuffix(addr, ":443"):
			return 1
		}
		return 2
	}
	slices.SortStableFunc(aps, func(a, b string) int { return rank(a) - rank(b) })
	m["accesspoint"], _ = json.Marshal(aps)
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}
