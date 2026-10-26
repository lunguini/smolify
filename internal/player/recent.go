package player

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	pbdata "github.com/devgianlu/go-librespot/proto/spotify/clienttoken/data/v0"
	pbhttp "github.com/devgianlu/go-librespot/proto/spotify/clienttoken/http/v0"
	"google.golang.org/protobuf/proto"
)

// Played is a list the user played lately: its URI, the track last played
// in it, and when.
type Played struct {
	ContextURI, TrackURI string
	At                   time.Time
}

// spclientHost answers Spotify's internal services for any account.
const spclientHost = "spclient.wg.spotify.com"

// maxRecentBytes bounds a recently played answer.
const maxRecentBytes = 1 << 20

// RecentlyPlayed is the lists username played lately, newest first, from
// Spotify's own recently played service — asked as go-librespot asks
// Spotify's services, with the session's token and a client token of its
// kind, not through the rate-limited Web API.
func (p *Player) RecentlyPlayed(ctx context.Context, username string, limit int) ([]Played, error) {
	token, err := p.Token(ctx)
	if err != nil {
		return nil, err
	}
	ct, err := p.ClientToken(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{"format": {"json"}, "offset": {"0"}, "limit": {fmt.Sprint(limit)}, "filter": {"default,collection-new-episodes"}}
	u := "https://" + spclientHost + "/recently-played/v3/user/" + url.PathEscape(username) + "/recently-played?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Token", ct)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", librespot.UserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("recently played: %s", resp.Status)
	}
	return parseRecent(io.LimitReader(resp.Body, maxRecentBytes))
}

// parseRecent reads a recently-played/v3 answer.
func parseRecent(r io.Reader) ([]Played, error) {
	var body struct {
		PlayContexts []struct {
			URI                string `json:"uri"`
			LastPlayedTime     int64  `json:"lastPlayedTime"`
			LastPlayedTrackURI string `json:"lastPlayedTrackUri"`
		} `json:"playContexts"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return nil, fmt.Errorf("recently played: %w", err)
	}
	out := make([]Played, 0, len(body.PlayContexts))
	for _, c := range body.PlayContexts {
		if c.URI == "" {
			continue
		}
		out = append(out, Played{ContextURI: c.URI, TrackURI: c.LastPlayedTrackURI, At: time.UnixMilli(c.LastPlayedTime)})
	}
	return out, nil
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

// ClientToken is a client token for this device, asked for as go-librespot
// asks for its own, and kept until it expires.
func (p *Player) ClientToken(ctx context.Context) (string, error) {
	p.ctMu.Lock()
	defer p.ctMu.Unlock()
	if p.ct != "" && time.Now().Before(p.ctUntil) {
		return p.ct, nil
	}
	st, err := fileStore{dir: p.dir()}.Load()
	if err != nil {
		return "", err
	}
	body, err := proto.Marshal(&pbhttp.ClientTokenRequest{
		RequestType: pbhttp.ClientTokenRequestType_REQUEST_CLIENT_DATA_REQUEST,
		Request: &pbhttp.ClientTokenRequest_ClientData{ClientData: &pbhttp.ClientDataRequest{
			ClientId:      librespot.ClientIdHex,
			ClientVersion: librespot.SpotifyLikeClientVersion(),
			Data: &pbhttp.ClientDataRequest_ConnectivitySdkData{ConnectivitySdkData: &pbdata.ConnectivitySdkData{
				DeviceId: st.DeviceId, PlatformSpecificData: librespot.GetPlatformSpecificData(),
			}},
		}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://clienttoken.spotify.com/v1/clienttoken", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/x-protobuf")
	req.Header.Set("User-Agent", librespot.UserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("client token: %s", resp.Status)
	}
	var out pbhttp.ClientTokenResponse
	if err := proto.Unmarshal(data, &out); err != nil {
		return "", err
	}
	g := out.GetGrantedToken()
	if g == nil || g.Token == "" {
		return "", errors.New("client token: not granted")
	}
	p.ct = g.Token
	p.ctUntil = time.Now().Add(time.Duration(max(g.ExpiresAfterSeconds-60, 60)) * time.Second)
	return p.ct, nil
}
