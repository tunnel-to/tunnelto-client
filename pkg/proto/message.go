package proto

import "net/http"

const (
	TypeRegisterTunnel = "register_tunnel"
	TypeRegistered     = "registered"

	TypeRequestStart = "request_start"
	TypeRequestBody  = "request_body"
	TypeRequestEnd   = "request_end"

	TypeResponseStart = "response_start"
	TypeResponseBody  = "response_body"
	TypeResponseEnd   = "response_end"

	TypeWSStart   = "ws_start"
	TypeWSReady   = "ws_ready"
	TypeWSMessage = "ws_message"
	TypeWSClose   = "ws_close"

	TypeStreamError = "stream_error"
	TypePing        = "ping"
	TypePong        = "pong"
)

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Message struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`

	TunnelName string `json:"tunnel_name,omitempty"`
	Target     string `json:"target,omitempty"`
	PublicURL  string `json:"public_url,omitempty"`
	Token      string `json:"token,omitempty"`

	Method  string   `json:"method,omitempty"`
	Path    string   `json:"path,omitempty"`
	Query   string   `json:"query,omitempty"`
	Headers []Header `json:"headers,omitempty"`
	HasBody bool     `json:"has_body,omitempty"`

	StatusCode      int    `json:"status_code,omitempty"`
	Body            []byte `json:"body,omitempty"`
	WebSocketOpcode int    `json:"websocket_opcode,omitempty"`

	Error string `json:"error,omitempty"`
}

var hopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

func HeadersFromHTTP(src http.Header) []Header {
	out := make([]Header, 0, len(src))
	for name, values := range src {
		if _, skip := hopHeaders[http.CanonicalHeaderKey(name)]; skip {
			continue
		}
		for _, value := range values {
			out = append(out, Header{Name: name, Value: value})
		}
	}
	return out
}

func ApplyHeaders(dst http.Header, headers []Header) {
	for _, header := range headers {
		if _, skip := hopHeaders[http.CanonicalHeaderKey(header.Name)]; skip {
			continue
		}
		dst.Add(header.Name, header.Value)
	}
}
