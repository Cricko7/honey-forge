package traffic

import (
	"context"
	"net"
	"sync"

	"honey-forge/internal/contract"
)

type Sink func(Frame)

type sinkKey struct{}

func WithSink(ctx context.Context, sink Sink) context.Context {
	return context.WithValue(ctx, sinkKey{}, sink)
}

// Mirror lets a non-Go listener preserve the same passive traffic links.
func Mirror(ctx context.Context, frame Frame) {
	if sink, ok := ctx.Value(sinkKey{}).(Sink); ok {
		sink(frame)
	}
}

// Listen installs a wire tap before any protocol decoder reads the connection.
func Listen(ctx context.Context, network, address string) (net.Listener, error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, network, address)
	if err != nil {
		return nil, err
	}

	sink, _ := ctx.Value(sinkKey{}).(Sink)
	if sink == nil {
		return listener, nil
	}

	return &tappedListener{Listener: listener, sink: sink}, nil
}

type tappedListener struct {
	net.Listener
	sink Sink
}

func (l *tappedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	return wrapConn(conn, l.sink), nil
}

type tappedConn struct {
	net.Conn
	mu     sync.Mutex // serializes frame emission from Read and Close
	sink   Sink
	frame  Frame
	closed bool
}

func wrapConn(conn net.Conn, sink Sink) *tappedConn {
	tapped := &tappedConn{
		Conn: conn,
		sink: sink,
		frame: Frame{
			ConnectionID:  string(contract.NewID()),
			RemoteAddress: conn.RemoteAddr().String(),
			LocalAddress:  conn.LocalAddr().String(),
		},
	}
	tapped.emit("open", nil)

	return tapped
}

func (c *tappedConn) emit(kind string, data []byte) {
	c.frame.Sequence++
	frame := c.frame
	frame.Kind = kind
	frame.Data = append([]byte(nil), data...)
	c.sink(frame)
}

func (c *tappedConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	if n > 0 {
		c.mu.Lock()
		if !c.closed {
			for offset := 0; offset < n; offset += MaxChunkBytes {
				c.emit("data", buffer[offset:min(n, offset+MaxChunkBytes)])
			}
		}

		c.mu.Unlock()
	}

	return n, err
}

func (c *tappedConn) Close() error {
	// Closing the underlying connection first interrupts a blocked Read.
	err := c.Conn.Close()
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.closed {
		c.closed = true
		c.emit("close", nil)
	}

	return err
}
