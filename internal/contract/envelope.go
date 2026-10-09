package contract

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin/binding"
)

const (
	WSCloseUnsupportedData = 1003
	WSCloseTooLarge        = 1009
)

// DecodeEnvelope validates text JSON only. WSS transports enforce frame limits,
// accumulated message limits and disabled compression before calling it.
func DecodeEnvelope(b []byte) (Envelope, error) {
	if len(b) > MaxBodyBytes {
		return Envelope{}, NewError("body_too_large")
	}
	if err := CheckJSON(b); err != nil {
		return Envelope{}, NewError("invalid_json")
	}
	var envelope Envelope
	if !strictShape(b, reflect.TypeOf(envelope)) {
		return Envelope{}, NewError("invalid_json")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&envelope); err != nil {
		return Envelope{}, NewError("invalid_json")
	}
	Configure()
	if err := binding.Validator.ValidateStruct(envelope); err != nil {
		return Envelope{}, NewError("validation_failed")
	}
	return envelope, nil
}
func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
