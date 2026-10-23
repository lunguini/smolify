// Command spike-web probes which of Spotify's internal endpoints answer the
// player's token for search and recently played, so Smolify can do both
// without the Web API's developer quotas. It prints each endpoint's status
// and the start of its answer; it never prints the token. Quit Smolify
// before running it (they share the sign-in).
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	pbdata "github.com/devgianlu/go-librespot/proto/spotify/clienttoken/data/v0"
	pbhttp "github.com/devgianlu/go-librespot/proto/spotify/clienttoken/http/v0"
	"google.golang.org/protobuf/proto"

	"github.com/lunguini/smolify/internal/player"
)

func main() {
	q := flag.String("q", "harder better", "the search to try")
	flag.Parse()

	dir, err := os.UserConfigDir()
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	p, err := player.Start(ctx, player.Options{Dir: filepath.Join(dir, "smolify"), DeviceName: "Smolify (probe)"})
	if err != nil {
		fail(err)
	}
	defer p.Close()
	if err := waitReady(ctx, p); err != nil {
		fail(err)
	}
	st, err := p.Status(ctx)
	if err != nil {
		fail(err)
	}
	token, err := p.Token(ctx)
	if err != nil {
		fail(err)
	}
	fmt.Printf("signed in; username known: %v, token length %d\n\n", st.Username != "", len(token))

	spclient := resolve(ctx, "spclient")
	fmt.Printf("spclient host: %s\n\n", spclient)
	user := url.PathEscape(st.Username)
	esc := url.PathEscape(*q)
	ct, err := clientToken(ctx)
	if err != nil {
		fmt.Printf("client token: %v\n\n", err)
	} else {
		fmt.Printf("client token: granted, length %d\n\n", len(ct))
	}
	type probe struct {
		name, method, url, body string
		clientToken, agent      bool
	}
	var probes []probe
	for _, country := range []string{"US", "RO", "GB", "DE"} {
		for _, host := range []string{spclient, "spclient.wg.spotify.com"} {
			u := "https://" + host + "/searchview/km/v4/search/" + esc + "?entityVersion=2&limit=3&imageSize=large&catalogue=premium&country=" + country + "&locale=en&username=" + user
			probes = append(probes, probe{"searchview v4 " + host + " country " + country, "GET", u, "", true, true})
		}
	}
	hc := &http.Client{Timeout: 20 * time.Second}
	for _, pr := range probes {
		req, err := http.NewRequestWithContext(ctx, pr.method, pr.url, strings.NewReader(pr.body))
		if err != nil {
			fail(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if pr.clientToken && ct != "" {
			req.Header.Set("Client-Token", ct)
		}
		if pr.agent {
			req.Header.Set("User-Agent", librespot.UserAgent())
			req.Header.Set("App-Platform", "OSX")
			req.Header.Set("Spotify-App-Version", librespot.SpotifyLikeClientVersion())
		}
		if pr.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := hc.Do(req)
		if err != nil {
			fmt.Printf("== %s\n   error: %v\n\n", pr.name, err)
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		fmt.Printf("== %s\n   %s  content-type %q  retry-after %q  %d bytes\n   %s\n",
			pr.name, resp.Status, resp.Header.Get("Content-Type"), resp.Header.Get("Retry-After"), len(data), excerpt(data))
		for _, h := range []string{"Www-Authenticate", "X-Error", "Grpc-Message", "Spotify-Error"} {
			if v := resp.Header.Get(h); v != "" {
				fmt.Printf("   %s: %s\n", h, v)
			}
		}
		fmt.Println()
	}
}

// waitReady waits for the player to sign in with the stored credentials.
func waitReady(ctx context.Context, p *player.Player) error {
	for {
		select {
		case ev := <-p.Events():
			switch e := ev.(type) {
			case player.Ready:
				return nil
			case player.Pairing:
				if e.Code != "" {
					return fmt.Errorf("not signed in: run smolify first and pair")
				}
			case player.Failed:
				return e.Err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// resolve asks Spotify's access point resolver for a host of kind.
func resolve(ctx context.Context, kind string) string {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://apresolve.spotify.com/?type="+kind, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fail(err)
	}
	defer resp.Body.Close()
	var out map[string][]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out[kind]) == 0 {
		fail(fmt.Errorf("apresolve: %v %v", err, out))
	}
	return strings.TrimSuffix(out[kind][0], ":443")
}

// clientToken asks Spotify for a client token, as go-librespot does for
// its own requests (session.retrieveClientToken, which it does not export).
func clientToken(ctx context.Context) (string, error) {
	id := make([]byte, 20)
	rand.Read(id)
	body, err := proto.Marshal(&pbhttp.ClientTokenRequest{
		RequestType: pbhttp.ClientTokenRequestType_REQUEST_CLIENT_DATA_REQUEST,
		Request: &pbhttp.ClientTokenRequest_ClientData{ClientData: &pbhttp.ClientDataRequest{
			ClientId:      librespot.ClientIdHex,
			ClientVersion: librespot.SpotifyLikeClientVersion(),
			Data: &pbhttp.ClientDataRequest_ConnectivitySdkData{ConnectivitySdkData: &pbdata.ConnectivitySdkData{
				DeviceId: hex.EncodeToString(id), PlatformSpecificData: librespot.GetPlatformSpecificData(),
			}},
		}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://clienttoken.spotify.com/v1/clienttoken", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/x-protobuf")
	req.Header.Set("User-Agent", librespot.UserAgent())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("clienttoken: %s", resp.Status)
	}
	var out pbhttp.ClientTokenResponse
	if err := proto.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.ResponseType != pbhttp.ClientTokenResponseType_RESPONSE_GRANTED_TOKEN_RESPONSE {
		return "", fmt.Errorf("clienttoken: %v", out.ResponseType)
	}
	return out.GetGrantedToken().Token, nil
}

// excerpt is the start of a response, on one line.
func excerpt(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return s
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "spike-web:", err)
	os.Exit(1)
}
