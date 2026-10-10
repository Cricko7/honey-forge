package postgres

// Keep the inspectable cause while making default boundary logging safe even
// when a driver parsing error embeds a connection string containing credentials.
type connectionError struct {
	message string
	cause   error
}

func (e *connectionError) Error() string { return e.message }
func (e *connectionError) Unwrap() error { return e.cause }
