package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/bse/notifyd/pkg/response"
)

type contextKey string

const TenantClaimsKey contextKey = "tenant_claims"

func Middleware(jwtMgr *JWTManager) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.Error(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				response.Error(w, http.StatusUnauthorized, "invalid authorization format")
				return
			}

			claims, err := jwtMgr.ValidateToken(parts[1])
			if err != nil {
				response.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), TenantClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetClaims(ctx context.Context) *TenantClaims {
	claims, _ := ctx.Value(TenantClaimsKey).(*TenantClaims)
	return claims
}
