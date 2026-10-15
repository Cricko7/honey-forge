package support

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"honey-forge/modules/auth"
)

var (
	ErrUnauthenticated = errors.New("support requires authentication")
	ErrInvalid         = errors.New("invalid support message")
	ErrUnavailable     = errors.New("support delivery unavailable")
)

type Message struct {
	Email   string `json:"email" validate:"required,email,max=254"`
	Subject string `json:"subject" validate:"required,max=160"`
	Message string `json:"message" validate:"required,max=5000"`
}

type Sender interface {
	Send(context.Context, Message) error
}

type Service struct {
	sender Sender
}

func NewService(sender Sender) *Service {
	return &Service{sender: sender}
}

func (s *Service) Send(ctx context.Context, actor auth.AuthContext, message Message) error {
	if actor.UserID == "" {
		return ErrUnauthenticated
	}

	message.Subject = strings.TrimSpace(message.Subject)
	message.Message = strings.TrimSpace(message.Message)

	if message.Subject == "" || message.Message == "" || strings.ContainsAny(message.Subject, "\r\n\x00") {
		return ErrInvalid
	}

	if s.sender == nil {
		return ErrUnavailable
	}

	if err := s.sender.Send(ctx, message); err != nil {
		return fmt.Errorf("deliver support message: %w", err)
	}

	return nil
}
