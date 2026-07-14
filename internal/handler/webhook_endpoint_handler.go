package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/auth"
	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/service"
	"github.com/bse/notifyd/pkg/response"
)

type WebhookEndpointHandler struct {
	svc *service.WebhookEndpointService
}

func NewWebhookEndpointHandler(svc *service.WebhookEndpointService) *WebhookEndpointHandler {
	return &WebhookEndpointHandler{svc: svc}
}

func (h *WebhookEndpointHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	endpoints, err := h.svc.ListByTenant(r.Context(), claims.TenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	response.JSON(w, http.StatusOK, endpoints)
}

func (h *WebhookEndpointHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var input domain.CreateWebhookEndpointInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	endpoint, secret, err := h.svc.Create(r.Context(), claims.TenantID, input)
	if err != nil {
		if errors.Is(err, domain.ErrValidationFailed) {
			response.Error(w, http.StatusBadRequest, "validation failed")
			return
		}
		if errors.Is(err, domain.ErrWebhookLimitReached) {
			response.Error(w, http.StatusForbidden, "WEBHOOK_LIMIT_REACHED")
			return
		}
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response.JSON(w, http.StatusCreated, newWebhookEndpointCreatedResponse(endpoint, secret))
}

func (h *WebhookEndpointHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "webhookID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid webhook ID")
		return
	}

	var input domain.UpdateWebhookEndpointInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.svc.Update(r.Context(), id, claims.TenantID, input)
	if err != nil {
		if errors.Is(err, domain.ErrValidationFailed) {
			response.Error(w, http.StatusBadRequest, "validation failed")
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			response.Error(w, http.StatusNotFound, "webhook endpoint not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response.JSON(w, http.StatusOK, updated)
}

func (h *WebhookEndpointHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "webhookID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid webhook ID")
		return
	}

	if err := h.svc.Delete(r.Context(), id, claims.TenantID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.Error(w, http.StatusNotFound, "webhook endpoint not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// webhookEndpointCreatedResponse is the POST /webhooks response shape: the
// endpoint fields plus the plaintext secret, present here and only here.
// Every other read goes through domain.WebhookEndpoint directly, whose
// Secret field is tagged json:"-".
type webhookEndpointCreatedResponse struct {
	*domain.WebhookEndpoint
	Secret string `json:"secret"`
}

func newWebhookEndpointCreatedResponse(endpoint *domain.WebhookEndpoint, secret string) webhookEndpointCreatedResponse {
	return webhookEndpointCreatedResponse{WebhookEndpoint: endpoint, Secret: secret}
}
