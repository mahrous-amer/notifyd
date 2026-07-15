package router

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/bse/notifyd/internal/auth"
	"github.com/bse/notifyd/internal/handler"
)

func maxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

func New(
	jwtMgr *auth.JWTManager,
	tenantH *handler.TenantHandler,
	channelH *handler.ChannelHandler,
	notifH *handler.NotificationHandler,
	authH *handler.AuthHandler,
	healthH *handler.HealthHandler,
	entH *handler.EntitlementHandler,
	svcHMACSecret string,
	quotaMW func(http.Handler) http.Handler,
	apiKeyH *handler.APIKeyHandler,
	webhookH *handler.WebhookEndpointHandler,
) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(maxBodySize(1 << 20)) // 1 MiB request body limit
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/health", healthH.Health)
	r.Post("/auth/token", authH.IssueToken)

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(jwtMgr))

		r.Route("/keys", func(r chi.Router) {
			r.Get("/", apiKeyH.List)
			r.Post("/", apiKeyH.Create)
			r.Delete("/{keyID}", apiKeyH.Revoke)
		})

		r.Route("/channels", func(r chi.Router) {
			r.Get("/", channelH.List)
			r.Post("/", channelH.Create)
			r.Route("/{channelID}", func(r chi.Router) {
				r.Get("/", channelH.GetByID)
				r.Patch("/", channelH.Update)
				r.Delete("/", channelH.Delete)
			})
		})

		r.Route("/webhooks", func(r chi.Router) {
			r.Get("/", webhookH.List)
			r.Post("/", webhookH.Create)
			r.Route("/{webhookID}", func(r chi.Router) {
				r.Put("/", webhookH.Update)
				r.Delete("/", webhookH.Delete)
			})
		})

		r.Route("/notifications", func(r chi.Router) {
			r.With(quotaMW).Post("/send", notifH.Send)
			r.With(quotaMW).Post("/send-multi", notifH.SendMulti)
			r.Get("/", notifH.List)
			r.Route("/{notificationID}", func(r chi.Router) {
				r.Get("/", notifH.GetByID)
				r.Get("/attempts", notifH.ListAttempts)
				r.Get("/metrics", notifH.GetMetrics)
			})
		})
	})

	r.Route("/admin", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(jwtMgr))
			r.Use(auth.AdminMiddleware())
			r.Route("/tenants", func(r chi.Router) {
				r.Get("/", tenantH.List)
				r.Post("/", tenantH.Create)
				r.Get("/by-slug/{slug}", tenantH.GetBySlug)
				r.Route("/{tenantID}", func(r chi.Router) {
					r.Get("/", tenantH.GetByID)
					r.Patch("/", tenantH.Update)
					r.Delete("/", tenantH.Delete)
				})
			})
		})
		// Service-to-service routes (billing -> notifyd), HMAC-authenticated.
		r.Group(func(r chi.Router) {
			r.Use(auth.HMACMiddleware(svcHMACSecret))
			r.Put("/tenants/{tenantID}/entitlements", entH.Put)
			r.Get("/tenants/{tenantID}/usage", entH.Usage)
		})
	})

	return r
}
