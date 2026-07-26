package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"
)

// DiscoveryURL is the one stable address in the whole project. The backend is
// reachable only through a Cloudflare quick tunnel, whose hostname changes
// every time the process restarts — so nothing the jury ever touches may
// contain it. Installed binaries resolve the current tunnel from here at
// startup, which turns a rotated tunnel into a one-line commit on the landing
// repo instead of a re-release.
const DiscoveryURL = "https://teamflashackaton30x.com/secura-endpoint.json"

// DefaultAPIURL is the local-dev fallback, used when discovery is unreachable.
const DefaultAPIURL = "http://localhost:8099"

type Endpoint struct {
	APIURL      string `json:"api_url"`
	WebTerminal string `json:"web_terminal"`
	Updated     string `json:"updated"`
}

// Discover fetches the published endpoint. It is deliberately short-timeout and
// non-fatal: a juror on a slow network must not stare at a hung splash, and a
// developer on localhost must not need the internet to run the CLI.
func Discover(ctx context.Context) (Endpoint, error) {
	var ep Endpoint
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DiscoveryURL, nil)
	if err != nil {
		return ep, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ep, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ep, &APIError{Status: resp.StatusCode, Message: "discovery no disponible"}
	}
	// Cloudflare Pages answers unknown paths with the SPA's index.html rather
	// than a 404, so a missing file arrives as a 200 full of HTML. Decoding is
	// what actually catches that.
	if err := json.NewDecoder(resp.Body).Decode(&ep); err != nil {
		return Endpoint{}, err
	}
	return ep, nil
}

// ResolveAPIURL applies the precedence: explicit flag > env > discovery >
// localhost. flagSet distinguishes "the user passed --api-url" from "cobra
// handed us its default", which is the only reason discovery can be automatic
// without ever overriding an operator's intent.
func ResolveAPIURL(ctx context.Context, flagValue string, flagSet bool) string {
	if flagSet && flagValue != "" {
		return flagValue
	}
	if v := envAPIURL(); v != "" {
		return v
	}
	if ep, err := Discover(ctx); err == nil && ep.APIURL != "" {
		return ep.APIURL
	}
	if flagValue != "" {
		return flagValue
	}
	return DefaultAPIURL
}

func envAPIURL() string { return os.Getenv("SECURA_API_URL") }
