package support

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const recipient = "romanjigg@gmail.com"

type SMTP struct {
	host     string
	port     string
	security string
	username string
	password string
	from     string
}

func SMTPFromEnvironment(getenv func(string) string) (*SMTP, error) {
	config := &SMTP{
		host:     strings.TrimSpace(getenv("SUPPORT_SMTP_HOST")),
		port:     strings.TrimSpace(getenv("SUPPORT_SMTP_PORT")),
		security: strings.TrimSpace(getenv("SUPPORT_SMTP_SECURITY")),
		username: getenv("SUPPORT_SMTP_USERNAME"),
		password: getenv("SUPPORT_SMTP_PASSWORD"),
		from:     strings.TrimSpace(getenv("SUPPORT_SMTP_FROM")),
	}

	if *config == (SMTP{}) {
		return nil, nil
	}

	if config.security == "" {
		config.security = "starttls"
	}

	if config.port == "" {
		config.port = "587"
		if config.security == "tls" {
			config.port = "465"
		}
	}

	port, err := strconv.Atoi(config.port)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("SUPPORT_SMTP_PORT must be between 1 and 65535")
	}

	if config.security != "starttls" && config.security != "tls" {
		return nil, fmt.Errorf("SUPPORT_SMTP_SECURITY must be starttls or tls")
	}

	from, err := mail.ParseAddress(config.from)
	if err != nil || from.Address != config.from || strings.ContainsAny(config.from, "\r\n") {
		return nil, fmt.Errorf("SUPPORT_SMTP_FROM must be a plain email address")
	}

	if config.host == "" || strings.ContainsAny(config.host, "/\r\n ") || config.username == "" || config.password == "" {
		return nil, fmt.Errorf("SUPPORT_SMTP_HOST, SUPPORT_SMTP_USERNAME and SUPPORT_SMTP_PASSWORD are required")
	}

	return config, nil
}

func (s *SMTP) Send(ctx context.Context, message Message) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.host, s.port))
	if err != nil {
		return fmt.Errorf("connect SMTP: %w", err)
	}
	defer conn.Close()

	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set SMTP deadline: %w", err)
	}

	tlsConfig := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
	var transport net.Conn = conn
	if s.security == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("SMTP TLS handshake: %w", err)
		}

		transport = secure
	}

	client, err := smtp.NewClient(transport, s.host)
	if err != nil {
		return fmt.Errorf("initialize SMTP: %w", err)
	}
	defer client.Close()

	if s.security == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}

	if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
		return fmt.Errorf("authenticate SMTP: %w", err)
	}

	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("SMTP sender: %w", err)
	}

	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("SMTP recipient: %w", err)
	}

	data, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP data: %w", err)
	}

	var body strings.Builder
	encoder := quotedprintable.NewWriter(&body)
	if _, err := encoder.Write([]byte(message.Message)); err != nil {
		return fmt.Errorf("encode support message: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("finish support encoding: %w", err)
	}

	content := "From: " + s.from + "\r\nTo: " + recipient + "\r\nReply-To: " + message.Email +
		"\r\nSubject: " + mime.QEncoding.Encode("utf-8", "HoneyForge: "+message.Subject) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" + body.String() + "\r\n"

	if _, err := data.Write([]byte(content)); err != nil {
		return fmt.Errorf("write SMTP message: %w", err)
	}

	if err := data.Close(); err != nil {
		return fmt.Errorf("SMTP acceptance: %w", err)
	}

	// DATA acceptance is the success boundary; a later disconnect must not suggest retrying.
	return nil
}
