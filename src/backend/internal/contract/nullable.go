package contract

import "encoding/json"

// Nullable distinguishes an absent field from a present JSON null. Use
// validate:"present" for T|null fields which are mandatory in the wire contract.
type Nullable[T any] struct {
	Value *T
	set   bool
}

func (n Nullable[T]) Present() bool  { return n.set }
func (Nullable[T]) AllowsNull() bool { return true }
func (n *Nullable[T]) UnmarshalJSON(b []byte) error {
	var value *T
	if err := json.Unmarshal(b, &value); err != nil {
		return err
	}
	n.Value = value
	n.set = true
	return nil
}

func (n Nullable[T]) MarshalJSON() ([]byte, error) { return json.Marshal(n.Value) }
