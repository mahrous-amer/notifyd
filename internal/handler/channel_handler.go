package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/auth"
	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/service"
	"github.com/bse/notifyd/pkg/response"
)

type ChannelHandler struct {
	svc *service.ChannelService
}

func NewChannelHandler(svc *service.ChannelService) *ChannelHandler {
	return &ChannelHandler{svc: svc}
}

func (h *ChannelHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var input domain.CreateChannelConfigInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg, err := h.svc.Create(r.Context(), claims.TenantID, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	response.JSON(w, http.StatusCreated, cfg)
}

func (h *ChannelHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid channel ID")
		return
	}

	cfg, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusNotFound, "channel config not found")
		return
	}
	if cfg.TenantID != claims.TenantID {
		response.Error(w, http.StatusNotFound, "channel config not found")
		return
	}

	response.JSON(w, http.StatusOK, cfg)
}

func (h *ChannelHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	configs, err := h.svc.ListByTenant(r.Context(), claims.TenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response.JSON(w, http.StatusOK, configs)
}

func (h *ChannelHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid channel ID")
		return
	}

	var input domain.UpdateChannelConfigInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.svc.Update(r.Context(), id, claims.TenantID, input)
	if err != nil {
		response.Error(w, http.StatusNotFound, "channel config not found")
		return
	}

	response.JSON(w, http.StatusOK, updated)
}

func (h *ChannelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid channel ID")
		return
	}

	if err := h.svc.Delete(r.Context(), id, claims.TenantID); err != nil {
		response.Error(w, http.StatusNotFound, "channel config not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
