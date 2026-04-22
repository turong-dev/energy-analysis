package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
)

// StdioTransport implements MCP over standard input/output.
// It reads JSON-RPC requests from stdin and writes responses to stdout.
// All diagnostic output is sent to stderr to avoid corrupting the protocol.
type StdioTransport struct {
	server *Server
	reader io.Reader
	writer io.Writer
}

// NewStdioTransport creates a new stdio transport for the given server.
func NewStdioTransport(server *Server) *StdioTransport {
	return &StdioTransport{
		server: server,
		reader: os.Stdin,
		writer: os.Stdout,
	}
}

// Run starts the stdio transport loop. It blocks until the input is closed
// or the context is cancelled.
func (t *StdioTransport) Run(ctx context.Context) error {
	// Ensure all logging goes to stderr so we don't corrupt stdout protocol
	log.SetOutput(os.Stderr)

	scanner := bufio.NewScanner(t.reader)
	// Increase buffer size for large JSON messages (1MB)
	const maxCapacity = 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		response := t.server.HandleMessage(ctx, []byte(line))
		if response == nil {
			// Notification - no response needed
			continue
		}

		responseJSON, err := json.Marshal(response)
		if err != nil {
			log.Printf("[MCP stdio] failed to marshal response: %v", err)
			continue
		}

		if _, err := t.writer.Write(responseJSON); err != nil {
			log.Printf("[MCP stdio] failed to write response: %v", err)
			return err
		}
		if _, err := t.writer.Write([]byte("\n")); err != nil {
			log.Printf("[MCP stdio] failed to write newline: %v", err)
			return err
		}

		// Flush if possible
		if f, ok := t.writer.(interface{ Flush() error }); ok {
			_ = f.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}
