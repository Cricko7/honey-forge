package agentws

import "context"

type sessionKey struct{}
type sessionIdentity struct {
	Identity     Identity
	ConnectionID string
}

func WithSession(ctx context.Context, identity Identity, connectionID string) context.Context {
	return context.WithValue(ctx, sessionKey{}, sessionIdentity{identity, connectionID})
}

// SessionFrom exposes the transport's authenticated fence to command storage.
func SessionFrom(ctx context.Context) (Identity, string, bool) {
	session, ok := ctx.Value(sessionKey{}).(sessionIdentity)
	return session.Identity, session.ConnectionID, ok
}
