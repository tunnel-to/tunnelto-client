package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/tunnel-to/tunnelto-client/pkg/proto"
)

func TestParseExposeArgsAllowsFlagsAfterTarget(t *testing.T) {
	opts, err := parseExposeArgs([]string{"http://localhost:3000", "--name", "claw", "--relay", "http://localhost:8080", "--region", "us-west", "--host-header", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.target != "http://localhost:3000" || opts.name != "claw" || opts.relay != "http://localhost:8080" || opts.region != "us-west" || opts.hostHeader != "localhost" {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestParseExposeArgsAcceptsUpstreamHostAlias(t *testing.T) {
	opts, err := parseExposeArgs([]string{"3000", "--upstream-host=rewrite"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.hostHeader != "rewrite" {
		t.Fatalf("hostHeader = %q; want rewrite", opts.hostHeader)
	}
}

func TestParseExposeArgsRejectsHostHeaderURL(t *testing.T) {
	if _, err := parseExposeArgs([]string{"3000", "--host-header", "http://localhost"}); err == nil {
		t.Fatal("expected URL-shaped host header to be rejected")
	}
}

func TestParseExposeArgsDefaultsToProductionRelay(t *testing.T) {
	t.Setenv("TUNNELTO_RELAY_URL", "")
	t.Setenv("TUNNELTO_API_URL", "")
	t.Setenv("TUNNELTO_TOKEN", "")
	opts, err := parseExposeArgs([]string{"3000"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.relay != defaultRelayURL {
		t.Fatalf("default relay = %q; want %q", opts.relay, defaultRelayURL)
	}
}

func TestParseExposeArgsDefaultsToProductionAPIWithToken(t *testing.T) {
	t.Setenv("TUNNELTO_RELAY_URL", "")
	t.Setenv("TUNNELTO_API_URL", "")
	t.Setenv("TUNNELTO_TOKEN", "tt_live_test")
	opts, err := parseExposeArgs([]string{"3000"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.api != "https://tunnel.to" {
		t.Fatalf("api = %q; want https://tunnel.to", opts.api)
	}
}

func TestParseExposeArgsAPIFlagOverridesTokenDefault(t *testing.T) {
	t.Setenv("TUNNELTO_RELAY_URL", "")
	t.Setenv("TUNNELTO_API_URL", "")
	t.Setenv("TUNNELTO_TOKEN", "tt_live_test")
	opts, err := parseExposeArgs([]string{"3000", "--api", "https://staging.tunnel.to/"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.api != "https://staging.tunnel.to" {
		t.Fatalf("api = %q; want override", opts.api)
	}
}

func TestNormalizeTargetArgument(t *testing.T) {
	tests := map[string]string{
		"3000":                  "http://localhost:3000",
		":3000":                 "http://localhost:3000",
		"localhost:3000":        "http://localhost:3000",
		"127.0.0.1:3000":        "http://127.0.0.1:3000",
		"[::1]:3000":            "http://[::1]:3000",
		"http://localhost:3000": "http://localhost:3000",
	}

	for input, want := range tests {
		got, err := normalizeTargetArgument(input)
		if err != nil {
			t.Fatalf("normalizeTargetArgument(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeTargetArgument(%q) = %q; want %q", input, got, want)
		}
	}
}

func TestLooksLikeTargetAllowsPortShorthand(t *testing.T) {
	if !looksLikeTarget("3000") {
		t.Fatal("expected numeric port to look like a target")
	}
	if looksLikeTarget("login") {
		t.Fatal("did not expect command names to look like targets")
	}
	if looksLikeTarget("foo:bar") {
		t.Fatal("did not expect invalid host:port to look like a target")
	}
}

func TestVersionDefaultIsSet(t *testing.T) {
	if version == "" {
		t.Fatal("expected version to have a default value")
	}
}

func TestNormalizeRelayURL(t *testing.T) {
	tests := map[string]string{
		"localhost:8080":              "ws://localhost:8080/connect",
		"http://localhost:8080":       "ws://localhost:8080/connect",
		"https://relay.tunnel.to":     "wss://relay.tunnel.to/connect",
		"wss://relay.tunnel.to/live":  "wss://relay.tunnel.to/live",
		"ws://localhost:8080/connect": "ws://localhost:8080/connect",
	}

	for input, want := range tests {
		got, err := normalizeRelayURL(input)
		if err != nil {
			t.Fatalf("normalizeRelayURL(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeRelayURL(%q) = %q; want %q", input, got, want)
		}
	}
}

func TestJoinURLPath(t *testing.T) {
	tests := []struct {
		base string
		req  string
		want string
	}{
		{base: "", req: "/v1/chat", want: "/v1/chat"},
		{base: "/", req: "/v1/chat", want: "/v1/chat"},
		{base: "/api", req: "/v1/chat", want: "/api/v1/chat"},
		{base: "/api/", req: "v1/chat", want: "/api/v1/chat"},
	}

	for _, tt := range tests {
		if got := joinURLPath(tt.base, tt.req); got != tt.want {
			t.Fatalf("joinURLPath(%q, %q) = %q; want %q", tt.base, tt.req, got, tt.want)
		}
	}
}

func TestPrepareUpstreamHeadersPreservesPublicHost(t *testing.T) {
	target := mustParseURL(t, "http://localhost:3000")
	client := &tunnelClient{target: target, hostHeader: "preserve"}
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Host", "claw.tunnel.to")
	req.Header.Set("X-Forwarded-Proto", "https")

	client.prepareUpstreamHeaders(req, req.Header)

	if req.Host != "claw.tunnel.to" {
		t.Fatalf("req.Host = %q; want public host", req.Host)
	}
	if got := req.Header.Get("Forwarded"); got != "host=claw.tunnel.to;proto=https" {
		t.Fatalf("Forwarded = %q; want derived public host/proto", got)
	}
	if got := req.Header.Get("Host"); got != "" {
		t.Fatalf("Host header should be removed from http.Header, got %q", got)
	}
}

func TestPrepareUpstreamHeadersRewritesExplicitHost(t *testing.T) {
	target := mustParseURL(t, "http://127.0.0.1:3000")
	client := &tunnelClient{target: target, hostHeader: "localhost"}
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Host", "claw.tunnel.to")

	client.prepareUpstreamHeaders(req, req.Header)

	if req.Host != "localhost" {
		t.Fatalf("req.Host = %q; want localhost", req.Host)
	}
	if got := req.Header.Get("X-Forwarded-Host"); got != "claw.tunnel.to" {
		t.Fatalf("X-Forwarded-Host = %q; want original public host preserved", got)
	}
}

func TestPrepareUpstreamHeadersDerivesTargetHost(t *testing.T) {
	target := mustParseURL(t, "http://127.0.0.1:3000")
	client := &tunnelClient{target: target, hostHeader: "rewrite"}
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		t.Fatal(err)
	}

	client.prepareUpstreamHeaders(req, req.Header)

	if req.Host != "127.0.0.1" {
		t.Fatalf("req.Host = %q; want target hostname without port", req.Host)
	}
}

func TestPrepareUpstreamHeaderValuesForWebSocket(t *testing.T) {
	target := mustParseURL(t, "http://localhost:3000")
	client := &tunnelClient{target: target, hostHeader: "localhost"}
	headers := http.Header{}
	headers.Set("X-Forwarded-Host", "claw.tunnel.to")
	headers.Set("X-Forwarded-Proto", "https")

	client.prepareUpstreamHeaderValues(headers)

	if got := headers.Get("Host"); got != "localhost" {
		t.Fatalf("Host = %q; want localhost", got)
	}
	if got := headers.Get("Forwarded"); got != "host=claw.tunnel.to;proto=https" {
		t.Fatalf("Forwarded = %q; want derived header", got)
	}
}

func TestWebSocketDialPreservesOriginAndPublicHost(t *testing.T) {
	type capturedHandshake struct {
		host      string
		origin    string
		protocol  string
		key       string
		forwarded string
	}
	captured := make(chan capturedHandshake, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		captured <- capturedHandshake{
			host:      req.Host,
			origin:    req.Header.Get("Origin"),
			protocol:  req.Header.Get("Sec-WebSocket-Protocol"),
			key:       req.Header.Get("Sec-WebSocket-Key"),
			forwarded: req.Header.Get("Forwarded"),
		}
		conn, err := upgrader.Upgrade(w, req, nil)
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()

	target := mustParseURL(t, server.URL)
	client := &tunnelClient{target: target, hostHeader: "preserve"}
	headers := websocketDialHeaders([]proto.Header{
		{Name: "Host", Value: "claw.tunnel.to"},
		{Name: "Origin", Value: "https://claw.tunnel.to"},
		{Name: "Sec-WebSocket-Key", Value: "browser-generated-key"},
		{Name: "Sec-WebSocket-Version", Value: "13"},
		{Name: "Sec-WebSocket-Protocol", Value: "openclaw.v1"},
		{Name: "X-Forwarded-Host", Value: "claw.tunnel.to"},
		{Name: "X-Forwarded-Proto", Value: "https"},
	})
	client.prepareUpstreamHeaderValues(headers)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	got := <-captured
	if got.host != "claw.tunnel.to" {
		t.Fatalf("Host = %q; want public tunnel host", got.host)
	}
	if got.origin != "https://claw.tunnel.to" {
		t.Fatalf("Origin = %q; want browser origin", got.origin)
	}
	if got.protocol != "openclaw.v1" {
		t.Fatalf("Sec-WebSocket-Protocol = %q; want preserved protocol", got.protocol)
	}
	if got.key == "browser-generated-key" || got.key == "" {
		t.Fatalf("Sec-WebSocket-Key = %q; want gorilla-generated connection key", got.key)
	}
	if got.forwarded != "host=claw.tunnel.to;proto=https" {
		t.Fatalf("Forwarded = %q; want derived forwarded header", got.forwarded)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
