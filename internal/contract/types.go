// Package contract owns the shared wire contract for REST and WSS modules.
package contract

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxBodyBytes    = 256 * 1024
	MaxConfigBytes  = 128 * 1024
	MaxCursorLength = 2048
	MaxRevision     = math.MaxInt32
	MaxFieldErrors  = 20
)

type ID string

// Use int64 at the JSON boundary so values above the domain limit produce
// validation errors (422), rather than integer decoding errors (400).
type Revision int64
type TypeVersion int64
type Timestamp time.Time

func (t Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t).UTC().Format(time.RFC3339Nano))
}
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("timestamp string: %w", err)
	}
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return NewError("validation_failed")
	}
	*t = Timestamp(v)
	return nil
}

type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (p Page[T]) MarshalJSON() ([]byte, error) {
	type wire Page[T]
	value := wire(p)
	if value.Items == nil {
		value.Items = []T{}
	}
	return json.Marshal(value)
}

func NewPage[T any](items []T, next *string) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{items, next}
}

type Envelope struct {
	MessageID ID                         `json:"message_id" validate:"required,uuid"`
	Type      string                     `json:"type" validate:"required,min=1,max=64"`
	ReplyTo   *ID                        `json:"reply_to" validate:"omitempty,uuid"`
	Payload   map[string]json.RawMessage `json:"payload" validate:"required"`
}

var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var typePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

func ValidID(s string) bool     { return idPattern.MatchString(s) }
func ValidTypeID(s string) bool { return typePattern.MatchString(s) }
func NewID() ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("generate request identifier: %w", err))
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return ID(fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]))
}
func NormalizeName(s string) string { return strings.TrimSpace(s) }
func ValidName(s string) bool {
	n := utf8.RuneCountInString(NormalizeName(s))
	return n >= 1 && n <= 100
}
func NextRevision(r Revision) (Revision, error) {
	if r < 1 || r > MaxRevision {
		return 0, NewError("validation_failed")
	}
	if r == MaxRevision {
		return 0, NewError("revision_exhausted")
	}
	return r + 1, nil
}
func CheckConfig(config json.RawMessage) error {
	if len(config) > MaxConfigBytes {
		return NewError("config_too_large")
	}
	return nil
}
func DecodeBytes(s string, max int) ([]byte, error) {
	b, err := base64.StdEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, NewError("validation_failed")
	}
	if base64.StdEncoding.EncodeToString(b) != s || len(b) > max {
		return nil, NewError("validation_failed")
	}
	return b, nil
}
