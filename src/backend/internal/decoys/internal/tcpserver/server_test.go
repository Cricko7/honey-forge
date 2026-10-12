package tcpserver

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestStopClosesListenersAndActiveSessions(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	finished := make(chan struct{})
	server := Start(t.Context(), []Listener{{Listener: listener, Serve: func(ctx context.Context, conn net.Conn) error {
		close(started)
		defer close(finished)

		var b [1]byte
		_, err := conn.Read(b[:])
		if ctx.Err() == nil {
			return err
		}

		return nil
	}}}, 64)
	t.Cleanup(server.Stop)
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	defer conn.Close()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not start")
	}

	server.Stop()
	select {
	case <-finished:
	default:
		t.Fatal("Stop returned before the session finished")
	}

	if server.Err() != nil {
		t.Fatal(server.Err())
	}

	if conn, err := net.Dial("tcp", listener.Addr().String()); err == nil {
		conn.Close()
		t.Fatal("listener still accepts after Stop")
	}
}

type scriptedListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *scriptedListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *scriptedListener) Addr() net.Addr {
	return &net.TCPAddr{}
}

func TestSessionLimitAndCancellationWhileSaturated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		listener := &scriptedListener{connections: make(chan net.Conn, 2), closed: make(chan struct{})}
		started := make(chan net.Conn, 2)
		server := Start(t.Context(), []Listener{{Listener: listener, Serve: func(ctx context.Context, conn net.Conn) error {
			started <- conn
			<-ctx.Done()

			return nil
		}}}, 1)
		for range 2 {
			serverConn, clientConn := net.Pipe()
			defer clientConn.Close()

			listener.connections <- serverConn
		}

		synctest.Wait()
		if len(started) != 1 {
			t.Fatal("session limit was not enforced")
		}

		server.Stop()
		if server.Err() != nil {
			t.Fatal(server.Err())
		}

		select {
		case <-listener.closed:
		default:
			t.Fatal("saturated listener did not close")
		}
	})
}

func TestSessionFailureStopsServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	cause := errors.New("telemetry unavailable")
	failed := make(chan struct{})
	server := Start(t.Context(), []Listener{{Listener: listener, Serve: func(context.Context, net.Conn) error {
		defer close(failed)

		return cause
	}}}, 64)
	t.Cleanup(server.Stop)
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	defer conn.Close()

	select {
	case <-failed:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not fail")
	}

	server.Stop()
	if !errors.Is(server.Err(), cause) {
		t.Fatalf("lost failure: %v", server.Err())
	}
}
