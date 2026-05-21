package main

import "testing"

func TestParseExposeArgsAllowsFlagsAfterTarget(t *testing.T) {
	opts, err := parseExposeArgs([]string{"http://localhost:3000", "--name", "claw", "--relay", "http://localhost:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.target != "http://localhost:3000" || opts.name != "claw" || opts.relay != "http://localhost:8080" {
		t.Fatalf("unexpected options: %#v", opts)
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
