package contract

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Name string
type Description string
type TypeID string
type Action = TypeID
type EventType = TypeID
type Bytes []byte

func (id *ID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if !ValidID(s) {
		return NewError("validation_failed")
	}
	*id = ID(strings.ToLower(s))
	return nil
}

func (n *Name) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if !ValidName(s) {
		return NewError("validation_failed")
	}
	*n = Name(NormalizeName(s))
	return nil
}

func (d *Description) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if utf8.RuneCountInString(s) > 1000 {
		return NewError("validation_failed")
	}
	*d = Description(s)
	return nil
}

func (t *TypeID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if !ValidTypeID(s) {
		return NewError("validation_failed")
	}
	*t = TypeID(s)
	return nil
}

func (r *Revision) UnmarshalJSON(b []byte) error {
	value, err := domainInteger(b)
	if err != nil {
		return err
	}
	*r = Revision(value)
	return nil
}

func (v *TypeVersion) UnmarshalJSON(b []byte) error {
	value, err := domainInteger(b)
	if err != nil {
		return err
	}
	*v = TypeVersion(value)
	return nil
}

func domainInteger(b []byte) (int64, error) {
	s := string(b)
	if s == "" {
		return 0, NewError("invalid_json")
	}
	for i, c := range s {
		if c == '-' && i == 0 {
			continue
		}
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("integer required")
		}
	}
	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil || value < 1 || value > MaxRevision {
		return 0, NewError("validation_failed")
	}
	return value, nil
}

func (b *Bytes) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	value, err := DecodeBytes(s, MaxBodyBytes)
	if err != nil {
		return err
	}
	*b = value
	return nil
}

func (Bytes) AllowsNull() bool { return false }
func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.StdEncoding.EncodeToString(b))
}
