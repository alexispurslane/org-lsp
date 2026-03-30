package lspstream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"go.lsp.dev/jsonrpc2"
)

// ReadWriteCloser wraps separate read and write closers into a single io.ReadWriteCloser
type ReadWriteCloser struct {
	Reader io.Reader
	Writer io.Writer
	Closer io.Closer
}

// NewReadWriteCloser creates a new ReadWriteCloser
func NewReadWriteCloser(r io.Reader, w io.Writer, c io.Closer) *ReadWriteCloser {
	return &ReadWriteCloser{Reader: r, Writer: w, Closer: c}
}

// Read implements io.Reader
func (rwc *ReadWriteCloser) Read(p []byte) (n int, err error) {
	return rwc.Reader.Read(p)
}

// Write implements io.Writer
func (rwc *ReadWriteCloser) Write(p []byte) (n int, err error) {
	return rwc.Writer.Write(p)
}

// Close implements io.Closer
func (rwc *ReadWriteCloser) Close() error {
	var err error
	if rwc.Closer != nil {
		err = rwc.Closer.Close()
	}
	return err
}

// LargeBufferStream wraps jsonrpc2.Stream with a larger buffer to handle big messages
type LargeBufferStream struct {
	conn io.ReadWriteCloser
	in   *bufio.Reader
}

// NewLargeBufferStream creates a new stream with a 128KB buffer (instead of default 4KB)
func NewLargeBufferStream(conn io.ReadWriteCloser) jsonrpc2.Stream {
	return &LargeBufferStream{
		conn: conn,
		in:   bufio.NewReaderSize(conn, 131072),
	}
}

func (s *LargeBufferStream) Read(ctx context.Context) (jsonrpc2.Message, int64, error) {
	// Read header lines - handles both CRLF (\r\n) and LF-only (\n) line endings
	// Simple approach: read until \n, then trim \r if present
	var total int64
	var length int64

	for {
		// Read until newline - this works for both CRLF and LF-only
		line, err := s.in.ReadString('\n')
		total += int64(len(line))
		if err != nil {
			return nil, total, fmt.Errorf("error reading header: %w", err)
		}

		// Trim trailing \r\n or just \n
		// After this: "Content-Length: 123\r\n" -> "Content-Length: 123"
		line = strings.TrimRight(line, "\r\n")

		// Empty line signals end of headers
		if line == "" {
			break
		}

		// Parse Content-Length header
		if after, ok := strings.CutPrefix(line, "Content-Length: "); ok {
			val := strings.TrimSpace(after)
			var parseErr error
			length, parseErr = strconv.ParseInt(val, 10, 32)
			if parseErr != nil {
				return nil, total, fmt.Errorf("failed parsing Content-Length: %w", parseErr)
			}
		}
	}

	if length == 0 {
		return nil, total, fmt.Errorf("missing Content-Length header")
	}

	// Read exactly 'length' bytes for the message body
	data := make([]byte, length)
	if _, err := io.ReadFull(s.in, data); err != nil {
		return nil, total, fmt.Errorf("error reading message body: %w", err)
	}
	total += length

	msg, err := jsonrpc2.DecodeMessage(data)
	return msg, total, err
}

func (s *LargeBufferStream) Write(ctx context.Context, msg jsonrpc2.Message) (int64, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return 0, err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := s.conn.Write([]byte(header)); err != nil {
		return 0, err
	}
	n, err := s.conn.Write(data)
	return int64(len(header)) + int64(n), err
}

func (s *LargeBufferStream) Close() error {
	return s.conn.Close()
}

// DebugLargeBufferStream is a debugging version of LargeBufferStream that logs all operations
type DebugLargeBufferStream struct {
	conn   io.ReadWriteCloser
	in     *bufio.Reader
	name   string
	logger *slog.Logger
}

// NewDebugLargeBufferStream creates a debugging stream with detailed logging
func NewDebugLargeBufferStream(conn io.ReadWriteCloser, name string) jsonrpc2.Stream {
	return &DebugLargeBufferStream{
		conn:   conn,
		in:     bufio.NewReaderSize(conn, 131072),
		name:   name,
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
}

func (s *DebugLargeBufferStream) Read(ctx context.Context) (jsonrpc2.Message, int64, error) {
	s.logger.Debug("=== Read() started", "stream", s.name, "buffered", s.in.Buffered())

	var total int64
	var length int64
	var headers []string

	for {
		line, err := s.in.ReadString('\n')
		total += int64(len(line))

		if err != nil {
			s.logger.Debug("Header read error", "stream", s.name, "error", err, "line_len", len(line))
			return nil, total, fmt.Errorf("error reading header: %w", err)
		}

		trimmed := strings.TrimRight(line, "\r\n")
		s.logger.Debug("Header line", "stream", s.name, "line", trimmed, "raw_len", len(line))

		if trimmed == "" {
			s.logger.Debug("End of headers", "stream", s.name, "header_count", len(headers))
			break
		}

		headers = append(headers, trimmed)

		if strings.HasPrefix(trimmed, "Content-Length: ") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "Content-Length: "))
			var parseErr error
			length, parseErr = strconv.ParseInt(val, 10, 32)
			if parseErr != nil {
				s.logger.Error("Failed to parse Content-Length", "stream", s.name, "value", val)
				return nil, total, fmt.Errorf("failed parsing Content-Length: %w", parseErr)
			}
			s.logger.Debug("Content-Length found", "stream", s.name, "length", length)
		}
	}

	if length == 0 {
		s.logger.Error("Missing Content-Length", "stream", s.name, "headers", headers)
		return nil, total, fmt.Errorf("missing Content-Length header (headers: %v)", headers)
	}

	s.logger.Debug("Reading body", "stream", s.name, "expected_length", length, "buffered", s.in.Buffered())

	data := make([]byte, length)
	if _, err := io.ReadFull(s.in, data); err != nil {
		s.logger.Error("Body read error", "stream", s.name, "error", err)
		return nil, total, fmt.Errorf("error reading message body: %w", err)
	}
	total += length

	preview := string(data)
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	s.logger.Debug("Body read complete", "stream", s.name, "length", length, "preview", preview)

	msg, err := jsonrpc2.DecodeMessage(data)
	if err != nil {
		s.logger.Error("Decode error", "stream", s.name, "error", err)
	} else {
		// Try to get method from Call or Notification
		var method string
		switch m := msg.(type) {
		case *jsonrpc2.Call:
			method = m.Method()
		case *jsonrpc2.Notification:
			method = m.Method()
		default:
			method = "(unknown)"
		}
		s.logger.Debug("Message decoded", "stream", s.name, "method", method)
	}
	return msg, total, err
}

func (s *DebugLargeBufferStream) Write(ctx context.Context, msg jsonrpc2.Message) (int64, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return 0, err
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	s.logger.Debug("Writing message", "stream", s.name, "header", strings.TrimSpace(header), "body_len", len(data))

	if _, err := s.conn.Write([]byte(header)); err != nil {
		return 0, err
	}
	n, err := s.conn.Write(data)
	return int64(len(header)) + int64(n), err
}

func (s *DebugLargeBufferStream) Close() error {
	s.logger.Debug("Closing stream", "stream", s.name)
	return s.conn.Close()
}
