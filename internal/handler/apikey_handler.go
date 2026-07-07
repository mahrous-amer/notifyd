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

type APIKeyHandler struct {
	svc *service.APIKeyService
}

func NewAPIKeyHandler(svc *service.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{svc: svc}
}

func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	keys, err := h.svc.ListByTenant(r.Context(), claims.TenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	response.JSON(w, http.StatusOK, keys)
}

func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	key, rawSecret, err := h.svc.Create(r.Context(), claims.TenantID, req.Label)
	if err != nil {
		if errors.Is(err, domain.ErrKeyLimitReached) {
			response.Error(w, http.StatusForbidden, "KEY_LIMIT_REACHED")
			return
		}
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{
		"key":        key,
		"api_secret": rawSecret, // shown once, never retrievable again
	})
}

func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "keyID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid key ID")
		return
	}
	if err := h.svc.Revoke(r.Context(), id, claims.TenantID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.Error(w, http.StatusNotFound, "key not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
