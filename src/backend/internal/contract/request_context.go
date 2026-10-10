package contract

import "context"

type methodKey struct{}

func WithHTTPMethod(ctx context.Context, method string) context.Context {
	return context.WithValue(ctx, methodKey{}, method)
}

func ReadOnlyRequest(ctx context.Context) bool {
	method, _ := ctx.Value(methodKey{}).(string)
	return method == "GET" || method == "HEAD" || method == "OPTIONS"
}
