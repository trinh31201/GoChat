package middleware

import (
	"context"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	netHttp "net/http"
)

// CORS returns a middleware that handles Cross-Origin Resource Sharing
func CORS() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if tr, ok := transport.FromServerContext(ctx); ok {
				if ht, ok := tr.(interface {
					ReplyHeader() netHttp.Header
					Request() *netHttp.Request
				}); ok {
					header := ht.ReplyHeader()
					header.Set("Access-Control-Allow-Origin", "*")
					header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
					header.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
					header.Set("Access-Control-Allow-Credentials", "true")
					header.Set("Access-Control-Max-Age", "86400")

					// Handle preflight OPTIONS requests
					if ht.Request().Method == "OPTIONS" {
						return nil, nil
					}
				}
			}
			return handler(ctx, req)
		}
	}
}
