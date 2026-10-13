package redistrap

import (
	"bufio"
	"errors"
	"io"
	"strconv"
	"strings"
)

var errFrame = errors.New("invalid Redis command frame")

func commandLine(r *bufio.Reader) (string, int64, error) {
	var line strings.Builder
	var count int64
	for {
		part, err := r.ReadSlice('\n')
		count += int64(len(part))
		if line.Len() < 65536 {
			line.Write(part[:min(len(part), 65536-line.Len())])
		}

		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}

		if count > 65536 {
			return line.String(), count, errFrame
		}

		return line.String(), count, err
	}
}

// Retain at most 8 KiB for telemetry; larger bulk values are streamed.
func readCommand(r *bufio.Reader) ([]string, int64, bool, error) {
	line, count, err := commandLine(r)
	if err != nil {
		return nil, count, false, err
	}

	if !strings.HasSuffix(line, "\r\n") {
		return nil, count, false, errFrame
	}

	if line[0] != '*' {
		args, err := inlineArgs(strings.TrimSuffix(line, "\r\n"))
		return args, count, count > 8192, err
	}

	items, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || items > 1<<20 {
		return nil, count, false, errFrame
	}

	if items <= 0 {
		return nil, count, false, nil
	}

	var args []string
	retained, truncated := 0, false
	for range items {
		header, n, err := commandLine(r)
		count += n
		if err != nil || len(header) < 4 || header[0] != '$' || !strings.HasSuffix(header, "\r\n") {
			return args, count, true, errors.Join(errFrame, err)
		}

		size, err := strconv.ParseInt(strings.TrimSpace(header[1:]), 10, 64)
		if err != nil || size < 0 || size > 512<<20 {
			return args, count, true, errFrame
		}

		keep := min(size, int64(max(0, 8192-retained)))
		value := make([]byte, keep)
		read, err := io.ReadFull(r, value)
		count += int64(read)
		if err != nil {
			return args, count, true, err
		}

		n, err = io.CopyN(io.Discard, r, size-keep)
		count += n
		if err != nil {
			return args, count, true, err
		}

		var end [2]byte
		read, err = io.ReadFull(r, end[:])
		count += int64(read)
		if err != nil || string(end[:]) != "\r\n" {
			return args, count, true, errors.Join(errFrame, err)
		}

		if len(args) < 64 {
			args = append(args, string(value))
		} else {
			truncated = true
		}

		retained += int(keep)
		truncated = truncated || keep < size
	}

	return args, count, truncated || count > 8192, nil
}

func inlineArgs(line string) ([]string, error) {
	var args []string
	for i := 0; i < len(line); {
		if line[i] == ' ' || line[i] == '\t' {
			i++
			continue
		}

		var word strings.Builder
		quote := byte(0)
		for i < len(line) {
			c := line[i]
			i++
			if quote == 0 && (c == ' ' || c == '\t') {
				break
			}

			if c == quote && quote != 0 {
				quote = 0
				if i < len(line) && line[i] != ' ' && line[i] != '\t' {
					return args, errFrame
				}

				break
			}

			if quote == 0 && (c == '\'' || c == '"') {
				quote = c
				continue
			}

			if c == '\\' && i < len(line) && quote != 0 {
				if quote == '\'' && line[i] != '\'' {
					word.WriteByte(c)
					continue
				}

				c = line[i]
				i++
				if quote == '\'' {
					word.WriteByte(c)
					continue
				}

				switch c {
				case 'n':
					c = '\n'
				case 'r':
					c = '\r'
				case 't':
					c = '\t'
				case 'b':
					c = '\b'
				case 'a':
					c = '\a'
				case 'x':
					if i+2 <= len(line) {
						if b, err := strconv.ParseUint(line[i:i+2], 16, 8); err == nil {
							c = byte(b)
							i += 2
						}
					}
				}
			}

			word.WriteByte(c)
		}

		if quote != 0 {
			return args, errFrame
		}

		args = append(args, word.String())
	}

	return args, nil
}
