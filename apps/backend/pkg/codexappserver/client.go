// Package codexappserver implements Codex app-server's JSON-RPC protocol.
package codexappserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	defaultMaxFrameBytes = 10 * 1024 * 1024
	defaultQueueSize     = 128
)

var ErrClosed = errors.New("codex app-server client closed")

// Options bounds individual frames and queued server events. Zero values use
// the protocol client's defaults.
type Options struct {
	MaxFrameBytes int
	QueueSize     int
}

// RPCError is an error returned by a JSON-RPC peer.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("Codex app-server RPC error %d: %s", e.Code, e.Message)
}

// RequestHandler answers JSON-RPC requests initiated by the app-server.
type RequestHandler func(context.Context, string, json.RawMessage) (any, error)

// NotificationHandler receives notifications initiated by the app-server.
type NotificationHandler func(context.Context, string, json.RawMessage)

// FrameDirection identifies which side of the app-server transport emitted a
// captured JSON-RPC frame.
type FrameDirection string

const (
	FrameSent     FrameDirection = "sent"
	FrameReceived FrameDirection = "received"
)

// FrameObserver receives each complete frame in transport order. Returning an
// error stops the client so a capture failure cannot silently lose evidence.
type FrameObserver func(FrameDirection, json.RawMessage) error

type response struct {
	result json.RawMessage
	err    error
}

type pendingCall struct {
	response chan response
}

type outboundFrame struct {
	ctx  context.Context
	data []byte
	done chan error
}

type inboundMessage struct {
	id      json.RawMessage
	method  string
	params  json.RawMessage
	request bool
}

type wireMessage struct {
	// Codex app-server uses JSON-RPC-shaped envelopes but omits the optional
	// jsonrpc version field on inbound messages.
	JSONRPC json.RawMessage `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

// Client multiplexes requests over caller-owned stdin/stdout. Close stops the
// protocol loops and closes those streams when their concrete types implement
// io.Closer; process ownership remains with the caller.
type Client struct {
	stdout io.Reader
	stdin  io.Writer
	reader *bufio.Reader

	maxFrameBytes int
	outbound      chan outboundFrame
	inbound       chan inboundMessage
	done          chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc

	mu              sync.Mutex
	closeOnce       sync.Once
	streamCloseOnce sync.Once
	closeErr        error
	streamCloseErr  error
	requestHandler  RequestHandler
	notifyHandler   NotificationHandler
	frameObserver   FrameObserver
	pending         map[string]pendingCall
	nextID          uint64
	unknownResponse atomic.Uint64
}

// NewClient starts a protocol reader and a serialized writer for the supplied
// streams. The client does not start or supervise an app-server process.
func NewClient(stdin io.Writer, stdout io.Reader, options Options) *Client {
	if options.MaxFrameBytes <= 0 {
		options.MaxFrameBytes = defaultMaxFrameBytes
	}
	if options.QueueSize <= 0 {
		options.QueueSize = defaultQueueSize
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		stdout:        stdout,
		stdin:         stdin,
		reader:        bufio.NewReaderSize(stdout, 64*1024),
		maxFrameBytes: options.MaxFrameBytes,
		outbound:      make(chan outboundFrame, options.QueueSize),
		inbound:       make(chan inboundMessage, options.QueueSize),
		done:          make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
		pending:       make(map[string]pendingCall),
	}
	go c.readLoop()
	go c.writeLoop()
	go c.dispatchLoop()
	return c
}

// SetRequestHandler installs the handler for requests sent by the server.
func (c *Client) SetRequestHandler(handler RequestHandler) {
	c.mu.Lock()
	c.requestHandler = handler
	c.mu.Unlock()
}

// SetNotificationHandler installs the handler for server notifications.
func (c *Client) SetNotificationHandler(handler NotificationHandler) {
	c.mu.Lock()
	c.notifyHandler = handler
	c.mu.Unlock()
}

// SetFrameObserver installs an observer for raw JSON-RPC frames.
func (c *Client) SetFrameObserver(observer FrameObserver) {
	c.mu.Lock()
	c.frameObserver = observer
	c.mu.Unlock()
}

// Call sends a request and decodes its result into result. Pass nil to discard
// the response body.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	raw, err := c.CallRaw(ctx, method, params)
	if err != nil || result == nil || len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return err
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	return nil
}

// CallRaw sends a request and returns the response JSON without interpreting
// provider-specific fields.
func (c *Client) CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if strings.TrimSpace(method) == "" {
		return nil, errors.New("JSON-RPC method is required")
	}
	if ctx == nil {
		return nil, errors.New("JSON-RPC call context is required")
	}
	if err := c.terminalError(); err != nil {
		return nil, err
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	id := strconv.FormatUint(atomic.AddUint64(&c.nextID, 1), 10)
	idJSON := json.RawMessage(id)
	key := "n:" + id
	pending := pendingCall{response: make(chan response, 1)}
	c.mu.Lock()
	if c.closeErr != nil {
		err := c.closeErr
		c.mu.Unlock()
		return nil, err
	}
	c.pending[key] = pending
	c.mu.Unlock()

	frame := map[string]any{"jsonrpc": "2.0", "id": idJSON, "method": method}
	if params != nil {
		frame["params"] = params
	}
	if err := c.send(ctx, frame); err != nil {
		c.removePending(key)
		return nil, err
	}
	select {
	case got := <-pending.response:
		if got.err != nil {
			return nil, got.err
		}
		return got.result, nil
	case <-ctx.Done():
		c.removePending(key)
		return nil, ctx.Err()
	case <-c.done:
		c.removePending(key)
		return nil, c.terminalError()
	}
}

// Notify sends a JSON-RPC notification without waiting for a response.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	if strings.TrimSpace(method) == "" {
		return errors.New("JSON-RPC method is required")
	}
	if params == nil {
		params = struct{}{}
	}
	return c.send(ctx, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// Done closes after a transport or protocol failure, or after Close is called.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns the terminal transport error, if one occurred.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}

// UnknownResponseCount reports responses whose request was canceled or is not
// known to this client. The count is bounded to one integer and contains no
// provider or user identifiers.
func (c *Client) UnknownResponseCount() uint64 { return c.unknownResponse.Load() }

// Close ends the protocol loops and closes any closable streams. It does not
// kill a process that owns those streams.
func (c *Client) Close() error {
	c.terminate(ErrClosed)
	c.streamCloseOnce.Do(func() {
		if closer, ok := c.stdout.(io.Closer); ok {
			c.streamCloseErr = errors.Join(c.streamCloseErr, closer.Close())
		}
		if closer, ok := c.stdin.(io.Closer); ok {
			c.streamCloseErr = errors.Join(c.streamCloseErr, closer.Close())
		}
	})
	return c.streamCloseErr
}

func (c *Client) send(ctx context.Context, frame any) error {
	if ctx == nil {
		return errors.New("JSON-RPC write context is required")
	}
	if err := c.terminalError(); err != nil {
		return err
	}
	if err := c.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("encode JSON-RPC frame: %w", err)
	}
	if len(data) > c.maxFrameBytes {
		return fmt.Errorf("JSON-RPC frame exceeds %d bytes", c.maxFrameBytes)
	}
	write := outboundFrame{ctx: ctx, data: append(data, '\n'), done: make(chan error, 1)}
	select {
	case c.outbound <- write:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.terminalError()
	}
	select {
	case err := <-write.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.terminalError()
	}
}

func (c *Client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case write := <-c.outbound:
			if err := write.ctx.Err(); err != nil {
				write.done <- err
				continue
			}
			var err error
			remaining := write.data
			for len(remaining) > 0 {
				var count int
				count, err = c.stdin.Write(remaining)
				if err != nil {
					break
				}
				if count == 0 {
					err = io.ErrShortWrite
					break
				}
				remaining = remaining[count:]
			}
			if err != nil {
				err = fmt.Errorf("write app-server frame: %w", err)
				write.done <- err
				c.terminate(err)
				return
			}
			if err := c.observeFrame(FrameSent, bytes.TrimSuffix(write.data, []byte{'\n'})); err != nil {
				write.done <- err
				c.terminate(err)
				return
			}
			write.done <- nil
		}
	}
}

func (c *Client) readLoop() {
	for {
		line, err := readLine(c.reader, c.maxFrameBytes)
		if err != nil {
			c.terminate(fmt.Errorf("read app-server frame: %w", err))
			return
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if err := c.observeFrame(FrameReceived, line); err != nil {
			c.terminate(err)
			return
		}
		message, err := decodeWireMessage(line)
		if err == nil {
			err = c.handleWireMessage(message)
		}
		if err != nil {
			c.terminate(err)
			return
		}
	}
}

func decodeWireMessage(line []byte) (wireMessage, error) {
	var message wireMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return wireMessage{}, fmt.Errorf("decode app-server frame: %w", err)
	}
	if len(message.JSONRPC) != 0 {
		var version string
		if err := json.Unmarshal(message.JSONRPC, &version); err != nil || version != "2.0" {
			return wireMessage{}, fmt.Errorf("unsupported JSON-RPC version %s", message.JSONRPC)
		}
	}
	if message.Method != "" {
		if len(message.Result) != 0 || message.Error != nil {
			return wireMessage{}, errors.New("app-server request contains response fields")
		}
		if len(message.ID) != 0 && (string(message.ID) == "null" || !validID(message.ID)) {
			return wireMessage{}, errors.New("app-server request has invalid id")
		}
		return message, nil
	}
	if len(message.ID) == 0 || !validID(message.ID) || (len(message.Result) == 0) == (message.Error == nil) {
		return wireMessage{}, errors.New("invalid app-server JSON-RPC response")
	}
	return message, nil
}

func (c *Client) handleWireMessage(message wireMessage) error {
	if message.Method != "" {
		return c.enqueueInbound(inboundMessage{method: message.Method, params: message.Params, id: message.ID, request: len(message.ID) != 0})
	}
	key, err := responseIDKey(message.ID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	pending, ok := c.pending[key]
	if ok {
		delete(c.pending, key)
	}
	c.mu.Unlock()
	if !ok {
		c.unknownResponse.Add(1)
		return nil
	}
	var rpcErr error
	if message.Error != nil {
		rpcErr = message.Error
	}
	pending.response <- response{result: message.Result, err: rpcErr}
	return nil
}

func (c *Client) enqueueInbound(message inboundMessage) error {
	select {
	case c.inbound <- message:
		return nil
	case <-c.done:
		return c.terminalError()
	default:
		return fmt.Errorf("app-server event queue exceeds %d messages", cap(c.inbound))
	}
}

func (c *Client) dispatchLoop() {
	for {
		select {
		case <-c.done:
			return
		case message := <-c.inbound:
			if message.request {
				c.dispatchRequest(message)
				continue
			}
			c.mu.Lock()
			handler := c.notifyHandler
			c.mu.Unlock()
			if handler != nil {
				handler(c.ctx, message.method, message.params)
			}
		}
	}
}

func (c *Client) dispatchRequest(message inboundMessage) {
	c.mu.Lock()
	requestHandler := c.requestHandler
	c.mu.Unlock()
	result, rpcErr := executeServerRequest(c.ctx, requestHandler, message)
	frame := map[string]any{"jsonrpc": "2.0", "id": message.id}
	if rpcErr != nil {
		frame["error"] = rpcErr
	} else {
		frame["result"] = result
	}
	if err := c.send(c.ctx, frame); err != nil && !errors.Is(err, ErrClosed) {
		c.terminate(err)
	}
}

func executeServerRequest(ctx context.Context, handler RequestHandler, message inboundMessage) (any, *RPCError) {
	if handler == nil {
		return nil, &RPCError{Code: -32601, Message: "method not found: " + message.method}
	}
	result, err := handler(ctx, message.method, message.params)
	if err == nil {
		return result, nil
	}
	var typed *RPCError
	if errors.As(err, &typed) {
		return nil, typed
	}
	return nil, &RPCError{Code: -32603, Message: "internal error"}
}

func (c *Client) removePending(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *Client) observeFrame(direction FrameDirection, frame []byte) error {
	c.mu.Lock()
	observer := c.frameObserver
	c.mu.Unlock()
	if observer == nil {
		return nil
	}
	if err := observer(direction, append(json.RawMessage(nil), frame...)); err != nil {
		return fmt.Errorf("record %s app-server frame: %w", direction, err)
	}
	return nil
}

func (c *Client) terminalError() error {
	select {
	case <-c.done:
	default:
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeErr != nil {
		return c.closeErr
	}
	return ErrClosed
}

func (c *Client) terminate(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closeErr = err
		pending := c.pending
		c.pending = make(map[string]pendingCall)
		c.mu.Unlock()
		c.cancel()
		close(c.done)
		for _, call := range pending {
			call.response <- response{err: err}
		}
	})
}

func readLine(reader *bufio.Reader, limit int) ([]byte, error) {
	line := make([]byte, 0, 4096)
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > limit+1 {
			return nil, fmt.Errorf("frame exceeds %d bytes", limit)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if len(line) == 0 {
			return nil, io.EOF
		}
		if line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) > limit {
			return nil, fmt.Errorf("frame exceeds %d bytes", limit)
		}
		return line, nil
	}
}

func validID(id json.RawMessage) bool {
	if len(id) == 0 || bytes.Equal(id, []byte("null")) {
		return false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	switch value.(type) {
	case string, json.Number:
		return true
	default:
		return false
	}
}

func responseIDKey(id json.RawMessage) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("decode JSON-RPC id: %w", err)
	}
	switch typed := value.(type) {
	case string:
		return "s:" + typed, nil
	case json.Number:
		return "n:" + typed.String(), nil
	default:
		return "", fmt.Errorf("unsupported JSON-RPC id %s", id)
	}
}
