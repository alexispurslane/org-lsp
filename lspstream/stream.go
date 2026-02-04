package lspstream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// NewLargeBufferStream creates a new stream with a 64KB buffer (instead of default 4KB)
func NewLargeBufferStream(conn io.ReadWriteCloser) jsonrpc2.Stream {
	return &LargeBufferStream{
		conn: conn,
		in:   bufio.NewReaderSize(conn, 65536),
	}
}

func (s *LargeBufferStream) Read(ctx context.Context) (jsonrpc2.Message, int64, error) {
	// Read header lines
	var total int64
	var length int64
	for {
		line, err := s.in.ReadString('\n')
		total += int64(len(line))
		if err != nil {
			return nil, total, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		colon := strings.IndexRune(line, ':')
		if colon >= 0 {
			name := line[:colon]
			value := strings.TrimSpace(line[colon+1:])
			if name == "Content-Length" {
				var err error
				length, err = strconv.ParseInt(value, 10, 32)
				if err != nil {
					return nil, total, fmt.Errorf("failed parsing Content-Length: %w", err)
				}
				if length <= 0 {
					return nil, total, fmt.Errorf("invalid Content-Length: %v", length)
				}
			}
		}
	}

	if length == 0 {
		return nil, total, fmt.Errorf("missing Content-Length header")
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(s.in, data); err != nil {
		return nil, total, err
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
