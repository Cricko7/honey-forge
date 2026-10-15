package support

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSMTPRefusesPlaintextAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	commands := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			commands <- "accept failed"
			return
		}
		defer conn.Close()

		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			commands <- "deadline failed"
			return
		}

		reader := bufio.NewReader(conn)
		if _, err := io.WriteString(conn, "220 localhost ESMTP\r\n"); err != nil {
			commands <- "greeting failed"
			return
		}

		hello, err := reader.ReadString('\n')
		if err != nil {
			commands <- "EHLO failed"
			return
		}

		if _, err := io.WriteString(conn, "250-localhost\r\n250 AUTH PLAIN\r\n"); err != nil {
			commands <- "capabilities failed"
			return
		}

		startTLS, err := reader.ReadString('\n')
		if err != nil {
			commands <- "STARTTLS failed"
			return
		}

		if _, err := io.WriteString(conn, "502 STARTTLS not supported\r\n"); err != nil {
			commands <- "STARTTLS rejection failed"
			return
		}

		remaining, err := io.ReadAll(reader)
		if err != nil {
			commands <- "connection did not close"
			return
		}

		commands <- hello + startTLS + string(remaining)
	}()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	sender := &SMTP{
		host:     host,
		port:     port,
		security: "starttls",
		username: "private-user",
		password: "private-password",
		from:     "support@example.com",
	}

	err = sender.Send(t.Context(), Message{
		Email:   "user@example.com",
		Subject: "Помощь",
		Message: "Не запускается ловушка",
	})
	if err == nil {
		t.Fatal("accepted an SMTP server without STARTTLS")
	}

	observed := <-commands
	if !strings.HasPrefix(observed, "EHLO ") || strings.Contains(observed, "AUTH") {
		t.Fatalf("unexpected plaintext commands: %q", observed)
	}
}

func TestSMTPCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	sender := &SMTP{host: "127.0.0.1", port: "1", security: "starttls"}
	if err := sender.Send(ctx, Message{}); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
