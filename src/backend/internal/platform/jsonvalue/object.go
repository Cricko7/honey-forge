package jsonvalue

import (
	"bytes"
	"encoding/json"
	"io"
)

func DecodeObject(raw []byte) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var object map[string]any
	if err := d.Decode(&object); err != nil {
		return nil, err
	}

	if _, err := d.Token(); err != io.EOF {
		return nil, &json.SyntaxError{}
	}

	return object, nil
}
