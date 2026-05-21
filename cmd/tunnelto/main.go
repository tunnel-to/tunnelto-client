package main

import (
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

	"github.com/gorilla/websocket"
	"github.com/tunnel-to/tunnelto-client/pkg/proto"
)

type options struct {
	target string
	name   string
	relay  string
	token  string
}

type tunnelClient struct {
	conn       *websocket.Conn
	target     *url.URL
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
	case "login":
		fmt.Println("login is not implemented yet; use TUNNELTO_TOKEN for now")
	case "status":
		fmt.Println("status is not implemented yet")
	case "stop":
		fmt.Println("press Ctrl-C in the running tunnelto expose process to stop the tunnel")
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("  tunnelto expose http://localhost:3000 [--name claw] [--relay http://localhost:8080]")
	fmt.Println("  tunnelto login")
	fmt.Println("  tunnelto status")
	fmt.Println("  tunnelto stop")
}

func runExpose(args []string) error {
	opts, err := parseExposeArgs(args)
	if err != nil {
		return err
	}

	target, err := url.Parse(opts.target)
	if err != nil {
		return err
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("target must be an http or https URL: %s", opts.target)
	}

	relayURL, err := normalizeRelayURL(opts.relay)
	if err != nil {
		return err
	}

	header := http.Header{}
	if opts.token != "" {
		header.Set("Authorization", "Bearer "+opts.token)
	}

	conn, _, err := websocket.DefaultDialer.Dial(relayURL, header)
	if err != nil {
		return err
	}
	defer conn.Close()

	c := &tunnelClient{
		conn:       conn,
		target:     target,
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
	fmt.Println(registered.PublicURL)
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
		relay: env("TUNNELTO_RELAY_URL", "http://localhost:8080"),
		token: os.Getenv("TUNNELTO_TOKEN"),
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
		case arg == "--token":
			i++
			if i >= len(args) {
				return opts, errors.New("--token requires a value")
			}
			opts.token = args[i]
		case strings.HasPrefix(arg, "--token="):
			opts.token = strings.TrimPrefix(arg, "--token=")
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
	return opts, nil
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
