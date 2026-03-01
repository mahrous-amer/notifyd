package auth

import (
	"net/http"

	"github.com/bse/notifyd/pkg/response"
)

// AdminMiddleware rejects requests whose JWT claims do not have IsAdmin set.
// It must be applied after the standard JWT Middleware, which populates the
// claims in the request context.
func AdminMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r.Context())
			if claims == nil || !claims.IsAdmin {
				response.Error(w, http.StatusForbidden, "admin access required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
