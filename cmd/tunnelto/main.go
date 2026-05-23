package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tunnel-to/tunnelto-client/pkg/proto"
)

type options struct {
	target     string
	name       string
	relay      string
	api        string
	token      string
	region     string
	hostHeader string
}

type apiRegisterResponse struct {
	Tunnel struct {
		Name string `json:"name"`
	} `json:"tunnel"`
	Relay struct {
		ConnectURL string `json:"connect_url"`
	} `json:"relay"`
	PublicURL    string `json:"public_url"`
	ConnectToken string `json:"connect_token"`
}

type tunnelClient struct {
	conn       *websocket.Conn
	target     *url.URL
	hostHeader string
	httpClient *http.Client

	writeMu sync.Mutex

	reqMu    sync.Mutex
	requests map[string]*localRequest
}

type localRequest struct {
	body *io.PipeWriter
	ws   *websocket.Conn
	wsMu sync.Mutex
}

const defaultRelayURL = "https://tor1.tunnel.to"
const defaultAPIURL = "https://tunnel.to"

var version = "dev"

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "expose":
		if err := runExpose(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "-h", "--help", "help":
		usage()
	case "-v", "--version", "version":
		fmt.Println("tunnelto " + version)
	case "login":
		fmt.Println("login is not implemented yet; use TUNNELTO_TOKEN for now")
	case "status":
		fmt.Println("status is not implemented yet")
	case "stop":
		fmt.Println("press Ctrl-C in the running tunnelto expose process to stop the tunnel")
	default:
		if looksLikeTarget(os.Args[1]) {
			if err := runExpose(os.Args[1:]); err != nil {
				log.Fatal(err)
			}
			return
		}
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("  tunnelto 3000 [--name claw] [--region us-west] [--host-header localhost]")
	fmt.Println("  tunnelto expose http://localhost:3000 [--name claw] [--host-header rewrite] [--relay https://tor1.tunnel.to]")
	fmt.Println("  tunnelto login")
	fmt.Println("  tunnelto status")
	fmt.Println("  tunnelto stop")
	fmt.Println()
	fmt.Println("Host header modes:")
	fmt.Println("  --host-header preserve     preserve the public host from the tunnel request (default)")
	fmt.Println("  --host-header localhost    send Host: localhost to the upstream app")
	fmt.Println("  --host-header rewrite      derive Host from the upstream target host")
	fmt.Println("  --host-header internal     send an explicit upstream host")
	fmt.Println("  --upstream-host is an alias for --host-header")
}

func runExpose(args []string) error {
	opts, err := parseExposeArgs(args)
	if err != nil {
		return err
	}

	targetRaw, err := normalizeTargetArgument(opts.target)
	if err != nil {
		return err
	}
	target, err := url.Parse(targetRaw)
	if err != nil {
		return err
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("target must be an http or https URL: %s", opts.target)
	}

	var registration apiRegisterResponse
	if opts.region != "" && opts.api == "" {
		opts.api = defaultAPIURL
	}
	if opts.api != "" {
		registration, err = registerTunnelWithAPI(opts, target.String())
		if err != nil {
			return err
		}
		if registration.Relay.ConnectURL != "" {
			opts.relay = registration.Relay.ConnectURL
		}
		if registration.ConnectToken != "" {
			opts.token = registration.ConnectToken
		}
		if opts.name == "" && registration.Tunnel.Name != "" {
			opts.name = registration.Tunnel.Name
		}
	}

	relayURL, err := normalizeRelayURL(opts.relay)
	if err != nil {
		return err
	}

	header := http.Header{}
	if opts.token != "" {
		header.Set("Authorization", "Bearer "+opts.token)
	}

	conn, res, err := websocket.DefaultDialer.Dial(relayURL, header)
	if err != nil {
		if res != nil {
			return fmt.Errorf("could not connect to relay %s: %w (HTTP %s)", relayURL, err, res.Status)
		}
		return fmt.Errorf("could not connect to relay %s: %w", relayURL, err)
	}
	defer conn.Close()

	c := &tunnelClient{
		conn:       conn,
		target:     target,
		hostHeader: opts.hostHeader,
		httpClient: &http.Client{Timeout: 0},
		requests:   make(map[string]*localRequest),
	}

	if err := c.write(proto.Message{
		Type:       proto.TypeRegisterTunnel,
		TunnelName: opts.name,
		Target:     target.String(),
		Token:      opts.token,
	}); err != nil {
		return err
	}

	var registered proto.Message
	if err := conn.ReadJSON(&registered); err != nil {
		return err
	}
	if registered.Type == proto.TypeStreamError {
		return errors.New(registered.Error)
	}
	if registered.Type != proto.TypeRegistered {
		return fmt.Errorf("unexpected relay message: %s", registered.Type)
	}

	fmt.Println("[ok] Connected")
	fmt.Println("[ok] Tunnel established")
	fmt.Println()
	if registration.PublicURL != "" {
		fmt.Println(registration.PublicURL)
	} else {
		fmt.Println(registered.PublicURL)
	}
	fmt.Println()
	fmt.Println("Forwarding to " + target.String())
	fmt.Println("Press Ctrl-C to stop.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	errCh := make(chan error, 1)

	go func() {
		errCh <- c.readLoop()
	}()

	select {
	case <-stop:
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "stopping"))
		return nil
	case err := <-errCh:
		return err
	}
}

func parseExposeArgs(args []string) (options, error) {
	opts := options{
		relay:      env("TUNNELTO_RELAY_URL", defaultRelayURL),
		api:        strings.TrimRight(os.Getenv("TUNNELTO_API_URL"), "/"),
		token:      os.Getenv("TUNNELTO_TOKEN"),
		region:     os.Getenv("TUNNELTO_REGION"),
		hostHeader: "preserve",
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--name":
			i++
			if i >= len(args) {
				return opts, errors.New("--name requires a value")
			}
			opts.name = args[i]
		case strings.HasPrefix(arg, "--name="):
			opts.name = strings.TrimPrefix(arg, "--name=")
		case arg == "--relay":
			i++
			if i >= len(args) {
				return opts, errors.New("--relay requires a value")
			}
			opts.relay = args[i]
		case strings.HasPrefix(arg, "--relay="):
			opts.relay = strings.TrimPrefix(arg, "--relay=")
		case arg == "--api":
			i++
			if i >= len(args) {
				return opts, errors.New("--api requires a value")
			}
			opts.api = strings.TrimRight(args[i], "/")
		case strings.HasPrefix(arg, "--api="):
			opts.api = strings.TrimRight(strings.TrimPrefix(arg, "--api="), "/")
		case arg == "--token":
			i++
			if i >= len(args) {
				return opts, errors.New("--token requires a value")
			}
			opts.token = args[i]
		case strings.HasPrefix(arg, "--token="):
			opts.token = strings.TrimPrefix(arg, "--token=")
		case arg == "--region":
			i++
			if i >= len(args) {
				return opts, errors.New("--region requires a value")
			}
			opts.region = args[i]
		case strings.HasPrefix(arg, "--region="):
			opts.region = strings.TrimPrefix(arg, "--region=")
		case arg == "--host-header" || arg == "--upstream-host":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s requires a value", arg)
			}
			opts.hostHeader = args[i]
		case strings.HasPrefix(arg, "--host-header="):
			opts.hostHeader = strings.TrimPrefix(arg, "--host-header=")
		case strings.HasPrefix(arg, "--upstream-host="):
			opts.hostHeader = strings.TrimPrefix(arg, "--upstream-host=")
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown option: %s", arg)
		default:
			if opts.target != "" {
				return opts, fmt.Errorf("unexpected argument: %s", arg)
			}
			opts.target = arg
		}
	}

	if opts.target == "" {
		return opts, errors.New("missing target URL")
	}
	opts.hostHeader = strings.TrimSpace(opts.hostHeader)
	if err := validateHostHeaderOption(opts.hostHeader); err != nil {
		return opts, err
	}
	return opts, nil
}

func looksLikeTarget(arg string) bool {
	if isPort(arg) {
		return true
	}
	return strings.Contains(arg, "://") || hasHostPortTarget(arg)
}

func normalizeTargetArgument(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("missing target URL")
	}
	if isPort(raw) {
		return "http://localhost:" + raw, nil
	}
	if strings.HasPrefix(raw, ":") && isPort(strings.TrimPrefix(raw, ":")) {
		return "http://localhost" + raw, nil
	}
	if !strings.Contains(raw, "://") && hasHostPortTarget(raw) {
		return "http://" + raw, nil
	}
	return raw, nil
}

func hasHostPortTarget(value string) bool {
	if strings.HasPrefix(value, "[") {
		end := strings.LastIndex(value, "]:")
		return end > 0 && isPort(value[end+2:])
	}
	host, port, ok := strings.Cut(value, ":")
	return ok && host != "" && !strings.Contains(port, ":") && isPort(port)
}

func isPort(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func registerTunnelWithAPI(opts options, targetHint string) (apiRegisterResponse, error) {
	payload := map[string]string{
		"name":             opts.name,
		"target_hint":      targetHint,
		"client_version":   version,
		"preferred_region": opts.region,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return apiRegisterResponse{}, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(opts.api, "/")+"/api/tunnels/register", bytes.NewReader(body))
	if err != nil {
		return apiRegisterResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tunnelto-client/"+version)
	if opts.token != "" {
		req.Header.Set("Authorization", "Bearer "+opts.token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return apiRegisterResponse{}, err
	}
	defer res.Body.Close()

	var out apiRegisterResponse
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return apiRegisterResponse{}, err
		}
		if out.Relay.ConnectURL == "" {
			return apiRegisterResponse{}, errors.New("control plane did not return a relay connect URL")
		}
		return out, nil
	}

	var errorPayload struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&errorPayload)
	if errorPayload.Error == "" {
		errorPayload.Error = res.Status
	}
	return apiRegisterResponse{}, fmt.Errorf("control plane registration failed: %s", errorPayload.Error)
}

func normalizeRelayURL(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("relay URL must use http, https, ws, or wss: %s", raw)
	}

	if u.Path == "" || u.Path == "/" {
		u.Path = "/connect"
	}
	return u.String(), nil
}

func env(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func (c *tunnelClient) readLoop() error {
	for {
		var msg proto.Message
		if err := c.conn.ReadJSON(&msg); err != nil {
			return err
		}

		switch msg.Type {
		case proto.TypeRequestStart:
			c.startHTTPRequest(msg)
		case proto.TypeRequestBody:
			c.writeRequestBody(msg)
		case proto.TypeRequestEnd:
			c.endRequestBody(msg.RequestID)
		case proto.TypeWSStart:
			c.startWebSocket(msg)
		case proto.TypeWSMessage:
			c.writeWebSocketMessage(msg)
		case proto.TypeWSClose:
			c.closeWebSocket(msg.RequestID)
		case proto.TypePing:
			_ = c.write(proto.Message{Type: proto.TypePong})
		}
	}
}

func (c *tunnelClient) startHTTPRequest(msg proto.Message) {
	var body io.Reader
	var bodyWriter *io.PipeWriter

	if msg.HasBody {
		bodyReader, writer := io.Pipe()
		body = bodyReader
		bodyWriter = writer
		c.setRequest(msg.RequestID, &localRequest{body: writer})
	}

	go c.doHTTPRequest(msg, body, bodyWriter)
}

func (c *tunnelClient) doHTTPRequest(msg proto.Message, body io.Reader, bodyWriter *io.PipeWriter) {
	localURL := c.localHTTPURL(msg.Path, msg.Query)
	req, err := http.NewRequest(msg.Method, localURL, body)
	if err != nil {
		c.closeBodyWithError(bodyWriter, err)
		_ = c.write(proto.Message{Type: proto.TypeStreamError, RequestID: msg.RequestID, Error: err.Error()})
		c.deleteRequest(msg.RequestID)
		return
	}
	proto.ApplyHeaders(req.Header, msg.Headers)
	c.prepareUpstreamHeaders(req, req.Header)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.closeBodyWithError(bodyWriter, err)
		_ = c.write(proto.Message{Type: proto.TypeStreamError, RequestID: msg.RequestID, Error: err.Error()})
		c.deleteRequest(msg.RequestID)
		return
	}
	defer resp.Body.Close()

	if err := c.write(proto.Message{
		Type:       proto.TypeResponseStart,
		RequestID:  msg.RequestID,
		StatusCode: resp.StatusCode,
		Headers:    proto.HeadersFromHTTP(resp.Header),
	}); err != nil {
		c.deleteRequest(msg.RequestID)
		return
	}

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if err := c.write(proto.Message{Type: proto.TypeResponseBody, RequestID: msg.RequestID, Body: chunk}); err != nil {
				c.deleteRequest(msg.RequestID)
				return
			}
		}
		if errors.Is(readErr, io.EOF) {
			_ = c.write(proto.Message{Type: proto.TypeResponseEnd, RequestID: msg.RequestID})
			c.deleteRequest(msg.RequestID)
			return
		}
		if readErr != nil {
			_ = c.write(proto.Message{Type: proto.TypeStreamError, RequestID: msg.RequestID, Error: readErr.Error()})
			c.deleteRequest(msg.RequestID)
			return
		}
	}
}

func (c *tunnelClient) writeRequestBody(msg proto.Message) {
	req := c.getRequest(msg.RequestID)
	if req == nil || req.body == nil {
		return
	}
	if _, err := req.body.Write(msg.Body); err != nil {
		_ = req.body.CloseWithError(err)
	}
}

func (c *tunnelClient) endRequestBody(requestID string) {
	req := c.getRequest(requestID)
	if req == nil || req.body == nil {
		return
	}
	_ = req.body.Close()
}

func (c *tunnelClient) closeBodyWithError(body *io.PipeWriter, err error) {
	if body != nil {
		_ = body.CloseWithError(err)
	}
}

func (c *tunnelClient) startWebSocket(msg proto.Message) {
	localURL := c.localWebSocketURL(msg.Path, msg.Query)
	header := websocketDialHeaders(msg.Headers)
	c.prepareUpstreamHeaderValues(header)

	conn, _, err := websocket.DefaultDialer.Dial(localURL, header)
	if err != nil {
		_ = c.write(proto.Message{Type: proto.TypeStreamError, RequestID: msg.RequestID, Error: err.Error()})
		return
	}

	req := &localRequest{ws: conn}
	c.setRequest(msg.RequestID, req)

	if err := c.write(proto.Message{Type: proto.TypeWSReady, RequestID: msg.RequestID}); err != nil {
		conn.Close()
		c.deleteRequest(msg.RequestID)
		return
	}

	go c.readLocalWebSocket(msg.RequestID, conn)
}

func websocketDialHeaders(headers []proto.Header) http.Header {
	out := http.Header{}
	for _, header := range headers {
		name := http.CanonicalHeaderKey(header.Name)
		if name == "Origin" || strings.HasPrefix(name, "Sec-Websocket-") {
			continue
		}
		out.Add(header.Name, header.Value)
	}
	return out
}

func validateHostHeaderOption(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("--host-header requires a non-empty value")
	}
	switch value {
	case "preserve", "rewrite":
		return nil
	}
	if strings.Contains(value, "://") || strings.ContainsAny(value, "/?#") {
		return fmt.Errorf("--host-header must be a host name, not a URL: %s", value)
	}
	if strings.ContainsAny(value, "\r\n\t ") {
		return fmt.Errorf("--host-header contains invalid whitespace: %q", value)
	}
	return nil
}

func (c *tunnelClient) prepareUpstreamHeaders(req *http.Request, headers http.Header) {
	c.prepareUpstreamHeaderValues(headers)
	if host := c.upstreamHost(headers); host != "" {
		req.Host = host
	}
	headers.Del("Host")
}

func (c *tunnelClient) prepareUpstreamHeaderValues(headers http.Header) {
	ensureForwardedHeader(headers)
	if host := c.upstreamHost(headers); host != "" {
		headers.Set("Host", host)
	}
}

func (c *tunnelClient) upstreamHost(headers http.Header) string {
	switch c.hostHeader {
	case "", "preserve":
		return firstHeaderValue(headers, "X-Forwarded-Host", "Host")
	case "rewrite":
		return targetHostHeader(c.target)
	default:
		return c.hostHeader
	}
}

func firstHeaderValue(headers http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func targetHostHeader(target *url.URL) string {
	host := target.Hostname()
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func ensureForwardedHeader(headers http.Header) {
	if headers.Get("Forwarded") != "" {
		return
	}
	host := strings.TrimSpace(headers.Get("X-Forwarded-Host"))
	proto := strings.TrimSpace(headers.Get("X-Forwarded-Proto"))
	if host == "" && proto == "" {
		return
	}

	parts := make([]string, 0, 2)
	if host != "" {
		parts = append(parts, "host="+forwardedHeaderValue(host))
	}
	if proto != "" {
		parts = append(parts, "proto="+forwardedHeaderValue(proto))
	}
	headers.Set("Forwarded", strings.Join(parts, ";"))
}

func forwardedHeaderValue(value string) string {
	if value == "" {
		return `""`
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '.', '-', '_', '~', ':':
			continue
		}
		escaped := strings.ReplaceAll(value, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return value
}

func (c *tunnelClient) readLocalWebSocket(requestID string, conn *websocket.Conn) {
	defer c.closeWebSocket(requestID)
	for {
		opcode, body, err := conn.ReadMessage()
		if err != nil {
			_ = c.write(proto.Message{Type: proto.TypeWSClose, RequestID: requestID})
			return
		}
		if err := c.write(proto.Message{
			Type:            proto.TypeWSMessage,
			RequestID:       requestID,
			WebSocketOpcode: opcode,
			Body:            body,
		}); err != nil {
			return
		}
	}
}

func (c *tunnelClient) writeWebSocketMessage(msg proto.Message) {
	req := c.getRequest(msg.RequestID)
	if req == nil || req.ws == nil {
		return
	}

	opcode := msg.WebSocketOpcode
	if opcode == 0 {
		opcode = websocket.BinaryMessage
	}

	req.wsMu.Lock()
	defer req.wsMu.Unlock()
	if err := req.ws.WriteMessage(opcode, msg.Body); err != nil {
		c.closeWebSocket(msg.RequestID)
	}
}

func (c *tunnelClient) closeWebSocket(requestID string) {
	req := c.getRequest(requestID)
	if req != nil && req.ws != nil {
		req.ws.Close()
	}
	c.deleteRequest(requestID)
}

func (c *tunnelClient) localHTTPURL(path, query string) string {
	u := *c.target
	u.Path = joinURLPath(c.target.Path, path)
	u.RawQuery = query
	return u.String()
}

func (c *tunnelClient) localWebSocketURL(path, query string) string {
	u := *c.target
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = joinURLPath(c.target.Path, path)
	u.RawQuery = query
	return u.String()
}

func joinURLPath(basePath, requestPath string) string {
	if requestPath == "" {
		requestPath = "/"
	}
	if basePath == "" || basePath == "/" {
		return requestPath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(requestPath, "/")
}

func (c *tunnelClient) write(msg proto.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(msg)
}

func (c *tunnelClient) setRequest(requestID string, req *localRequest) {
	c.reqMu.Lock()
	c.requests[requestID] = req
	c.reqMu.Unlock()
}

func (c *tunnelClient) getRequest(requestID string) *localRequest {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	return c.requests[requestID]
}

func (c *tunnelClient) deleteRequest(requestID string) {
	c.reqMu.Lock()
	delete(c.requests, requestID)
	c.reqMu.Unlock()
}
