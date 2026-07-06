package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/pkg/response"
)

// UsageQuerier is the slice of NotificationRepository this handler needs.
type UsageQuerier interface {
	UsageByTenant(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (*domain.UsageReport, error)
}

type EntitlementHandler struct {
	entRepo domain.EntitlementRepository
	usage   UsageQuerier
}

func NewEntitlementHandler(entRepo domain.EntitlementRepository, usage UsageQuerier) *EntitlementHandler {
	return &EntitlementHandler{entRepo: entRepo, usage: usage}
}

type putEntitlementsRequest struct {
	PlanCode        string    `json:"plan_code"`
	MessageLimit    int64     `json:"message_limit"`
	AllowedChannels []string  `json:"allowed_channels"`
	APIKeyLimit     int       `json:"api_key_limit"`
	RetentionDays   int       `json:"retention_days"`
	PeriodStart     time.Time `json:"period_start"`
	PeriodEnd       time.Time `json:"period_end"`
}

func (h *EntitlementHandler) Put(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	var req putEntitlementsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PlanCode == "" || req.MessageLimit < 0 || req.APIKeyLimit < 1 || req.RetentionDays < 1 ||
		!req.PeriodEnd.After(req.PeriodStart) {
		response.Error(w, http.StatusBadRequest, "validation failed")
		return
	}
	channels := make([]domain.ChannelType, len(req.AllowedChannels))
	for i, c := range req.AllowedChannels {
		ct := domain.ChannelType(c)
		if !domain.IsValidChannelType(ct) {
			response.Error(w, http.StatusBadRequest, "invalid channel: "+c)
			return
		}
		channels[i] = ct
	}

	ent := &domain.Entitlements{
		TenantID:        tenantID,
		PlanCode:        req.PlanCode,
		MessageLimit:    req.MessageLimit,
		AllowedChannels: channels,
		APIKeyLimit:     req.APIKeyLimit,
		RetentionDays:   req.RetentionDays,
		PeriodStart:     req.PeriodStart,
		PeriodEnd:       req.PeriodEnd,
	}
	if err := h.entRepo.Upsert(r.Context(), ent); err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	response.JSON(w, http.StatusOK, ent)
}

func (h *EntitlementHandler) Usage(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}
	from, err := time.Parse(time.RFC3339, r.URL.Query().Get("period_start"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid period_start")
		return
	}
	to, err := time.Parse(time.RFC3339, r.URL.Query().Get("period_end"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid period_end")
		return
	}

	report, err := h.usage.UsageByTenant(r.Context(), tenantID, from, to)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	response.JSON(w, http.StatusOK, report)
}
