package mcp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type messageFraming int

const (
	framingContentLength messageFraming = iota
	framingJSONLine
)

func readMCPMessage(r *bufio.Reader) ([]byte, messageFraming, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line == "" {
			return nil, framingContentLength, io.EOF
		}
		return nil, framingContentLength, err
	}

	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return []byte(trimmed), framingJSONLine, nil
	}

	payload, err := readContentLengthMessageAfterFirstLine(r, line)
	return payload, framingContentLength, err
}

func readContentLengthMessage(r *bufio.Reader) ([]byte, error) {
	payload, _, err := readMCPMessage(r)
	return payload, err
}

func readContentLengthMessageAfterFirstLine(r *bufio.Reader, firstLine string) ([]byte, error) {
	contentLength := -1
	line := firstLine
	for {
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}

		n, ok, err := parseContentLengthHeader(trimmed)
		if err != nil {
			return nil, err
		}
		if ok {
			contentLength = n
		}

		var lineErr error
		line, lineErr = r.ReadString('\n')
		if lineErr != nil {
			if errors.Is(lineErr, io.EOF) && line == "" {
				return nil, io.EOF
			}
			return nil, lineErr
		}
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeMCPMessage(w io.Writer, payload []byte, framing messageFraming) error {
	if framing == framingJSONLine {
		_, err := w.Write(append(payload, '\n'))
		return err
	}
	return writeContentLengthMessage(w, payload)
}

func writeContentLengthMessage(w io.Writer, payload []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func parseContentLengthHeader(line string) (int, bool, error) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return 0, false, fmt.Errorf("invalid MCP header line %q", line)
	}
	if !strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
		return 0, false, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, false, fmt.Errorf("invalid Content-Length %q", value)
	}
	return n, true, nil
}
