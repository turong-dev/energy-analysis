package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// generateID creates a random session/request ID.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// SSEHandler returns an HTTP handler for MCP SSE transport.
func (s *Server) SSEHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Create session
		sessionID := generateID()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		session := &Session{
			ID:      sessionID,
			WriteCh: make(chan []byte, 100),
			Ctx:     ctx,
			Cancel:  cancel,
		}

		s.sessionMu.Lock()
		s.sessions[sessionID] = session
		s.sessionMu.Unlock()

		defer func() {
			s.sessionMu.Lock()
			delete(s.sessions, sessionID)
			s.sessionMu.Unlock()
			close(session.WriteCh)
		}()

		// Send endpoint event
		endpointURL := fmt.Sprintf("/mcp/message?session_id=%s", sessionID)
		if err := writeSSEEvent(w, "endpoint", []byte(endpointURL)); err != nil {
			log.Printf("[MCP] failed to write endpoint: %v", err)
			return
		}

		// Flush headers
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Keep connection alive and send messages
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-session.WriteCh:
				if !ok {
					return
				}
				if err := writeSSEEvent(w, "message", msg); err != nil {
					log.Printf("[MCP] failed to write message: %v", err)
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			case <-ticker.C:
				// Send keepalive comment
				if _, err := fmt.Fprintf(w, ":keepalive\n\n"); err != nil {
					log.Printf("[MCP] failed to write keepalive: %v", err)
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
	}
}

// MessageHandler returns an HTTP handler for receiving MCP messages.
func (s *Server) MessageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get session ID
		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			http.Error(w, "Missing session_id", http.StatusBadRequest)
			return
		}

		s.sessionMu.RLock()
		session, ok := s.sessions[sessionID]
		s.sessionMu.RUnlock()

		if !ok {
			http.Error(w, "Invalid session", http.StatusBadRequest)
			return
		}

		// Read and process message
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Handle the message
		response := s.HandleMessage(session.Ctx, body)

		// If it's a notification (no response), return 202 Accepted
		if response == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Send response via SSE if there's a session
		responseJSON, err := json.Marshal(response)
		if err != nil {
			log.Printf("[MCP] failed to marshal response: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		select {
		case session.WriteCh <- responseJSON:
			w.WriteHeader(http.StatusAccepted)
		case <-time.After(5 * time.Second):
			http.Error(w, "Session timeout", http.StatusGatewayTimeout)
		}
	}
}

// writeSSEEvent writes an SSE event to the writer.
func writeSSEEvent(w io.Writer, event string, data []byte) error {
	// Split data into lines and prefix each with "data: "
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if _, err := fmt.Fprintf(w, "data: %s\n", scanner.Text()); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Write event type if specified
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}

	// Empty line to end the event
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}

	return nil
}

// InMemoryTransport provides an in-memory transport for testing.
type InMemoryTransport struct {
	server  *Server
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	pending map[string]chan *Response
}

// NewInMemoryTransport creates a test transport.
func NewInMemoryTransport(server *Server) *InMemoryTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &InMemoryTransport{
		server:  server,
		ctx:     ctx,
		cancel:  cancel,
		pending: make(map[string]chan *Response),
	}
}

// Send sends a message and waits for a response.
func (t *InMemoryTransport) Send(method string, params interface{}) (*Response, error) {
	id := generateID()

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		req.Params = data
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Create response channel
	respCh := make(chan *Response, 1)
	t.mu.Lock()
	t.pending[id] = respCh
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}()

	// Process message
	response := t.server.HandleMessage(t.ctx, reqJSON)

	// Handle notifications (no response)
	if response == nil {
		return nil, nil
	}

	return response, nil
}

// Close shuts down the transport.
func (t *InMemoryTransport) Close() {
	t.cancel()
}
