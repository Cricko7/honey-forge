package redistrap

import (
	"bufio"
	"errors"
	"io"
	"strconv"
	"strings"
)

type observedResponse struct {
	wire  string
	count int64
	kind  byte
}

func captureResponseChunks(reader *bufio.Reader, observer *sessionObserver) error {
	var buffer [4096]byte
	for {
		n, err := reader.Read(buffer[:])
		if n > 0 {
			if err := observer.reply(observedResponse{wire: string(buffer[:n]), count: int64(n)}); err != nil {
				return err
			}
		}

		if err != nil {
			return nil
		}
	}
}

// Capture a bounded sample while consuming the complete RESP2/RESP3 frame.
func readResponse(reader *bufio.Reader) (observedResponse, error) {
	c := &responseCapture{reader: reader}
	kind, err := c.frame(0)
	return observedResponse{wire: c.sample.String(), count: c.count, kind: kind}, err
}

type responseCapture struct {
	reader *bufio.Reader
	sample strings.Builder
	count  int64
}

func (c *responseCapture) retain(data []byte) {
	c.count += int64(len(data))
	if c.sample.Len() < 4096 {
		c.sample.Write(data[:min(len(data), 4096-c.sample.Len())])
	}
}

func (c *responseCapture) line() (string, error) {
	var header strings.Builder
	for {
		part, err := c.reader.ReadSlice('\n')
		c.retain(part)
		if header.Len()+len(part) > 65536 {
			return "", errFrame
		}

		header.Write(part)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}

		line := header.String()
		if err == nil && !strings.HasSuffix(line, "\r\n") {
			err = errFrame
		}

		return strings.TrimSuffix(line, "\r\n"), err
	}
}

func (c *responseCapture) frame(depth int) (byte, error) {
	if depth > 128 {
		return 0, errFrame
	}

	header, err := c.line()
	if err != nil || len(header) == 0 {
		return 0, errors.Join(errFrame, err)
	}

	kind := header[0]
	switch kind {
	case '+', '-', ':', '_', '#', ',', '(':
		return kind, nil
	case '$', '!', '=':
		size, err := strconv.ParseInt(header[1:], 10, 64)
		if err != nil || size < -1 || size > 512<<20 {
			return kind, errFrame
		}

		if size == -1 {
			return kind, nil
		}

		var buffer [8192]byte
		for remaining := size + 2; remaining > 0; {
			n, err := io.ReadFull(c.reader, buffer[:min(remaining, int64(len(buffer)))])
			c.retain(buffer[:n])
			remaining -= int64(n)
			if err != nil {
				return kind, err
			}
		}

		return kind, nil
	case '*', '%', '~', '>', '|':
		size, err := strconv.ParseInt(header[1:], 10, 64)
		if err != nil || size < -1 || size > 1<<30 {
			return kind, errFrame
		}

		if kind == '%' || kind == '|' {
			size *= 2
		}

		for i := int64(0); i < size; i++ {
			if _, err := c.frame(depth + 1); err != nil {
				return kind, err
			}
		}

		return kind, nil
	default:
		return kind, errFrame
	}
}
