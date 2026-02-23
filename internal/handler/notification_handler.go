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

type NotificationHandler struct {
	svc         *service.NotificationService
	attemptRepo domain.DeliveryAttemptRepository
}

func NewNotificationHandler(svc *service.NotificationService, attemptRepo domain.DeliveryAttemptRepository) *NotificationHandler {
	return &NotificationHandler{svc: svc, attemptRepo: attemptRepo}
}

func (h *NotificationHandler) Send(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var input domain.SendNotificationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if input.ChannelConfigID == uuid.Nil {
		response.Error(w, http.StatusBadRequest, "channel_config_id is required")
		return
	}
	if input.Body == "" {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	notif, err := h.svc.Send(r.Context(), claims.TenantID, input)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.Error(w, http.StatusNotFound, "channel config not found")
			return
		}
		if errors.Is(err, domain.ErrValidationFailed) {
			response.Error(w, http.StatusBadRequest, "validation failed")
			return
		}
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response.JSON(w, http.StatusAccepted, notif)
}

func (h *NotificationHandler) SendMulti(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var input domain.SendMultiInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(input.Channels) == 0 || len(input.Channels) > 50 {
		response.Error(w, http.StatusBadRequest, "channels count must be between 1 and 50")
		return
	}

	results, errs := h.svc.SendMulti(r.Context(), claims.TenantID, input)

	type multiResult struct {
		Sent   []*domain.Notification `json:"sent"`
		Errors []string               `json:"errors,omitempty"`
	}
	res := multiResult{Sent: results}
	for _, e := range errs {
		res.Errors = append(res.Errors, sanitizeNotificationError(e))
	}

	response.JSON(w, http.StatusAccepted, res)
}

func (h *NotificationHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "notificationID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid notification ID")
		return
	}

	notif, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.Error(w, http.StatusNotFound, "notification not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if notif.TenantID != claims.TenantID {
		response.Error(w, http.StatusNotFound, "notification not found")
		return
	}

	response.JSON(w, http.StatusOK, notif)
}

func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, offset := parsePagination(r)
	filter := domain.NotificationFilter{
		TenantID: claims.TenantID,
		Limit:    limit,
		Offset:   offset,
	}

	if s := r.URL.Query().Get("status"); s != "" {
		status := domain.NotificationStatus(s)
		if !domain.IsValidNotificationStatus(status) {
			response.Error(w, http.StatusBadRequest, "invalid status filter")
			return
		}
		filter.Status = &status
	}
	if c := r.URL.Query().Get("channel"); c != "" {
		ch := domain.ChannelType(c)
		if !domain.IsValidChannelType(ch) {
			response.Error(w, http.StatusBadRequest, "invalid channel filter")
			return
		}
		filter.Channel = &ch
	}

	notifications, total, err := h.svc.List(r.Context(), filter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response.JSON(w, http.StatusOK, response.ListResponse{
		Data:   notifications,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (h *NotificationHandler) ListAttempts(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "notificationID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid notification ID")
		return
	}

	notif, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.Error(w, http.StatusNotFound, "notification not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if notif.TenantID != claims.TenantID {
		response.Error(w, http.StatusNotFound, "notification not found")
		return
	}

	attempts, err := h.attemptRepo.ListByNotification(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response.JSON(w, http.StatusOK, attempts)
}

// sanitizeNotificationError returns a client-safe error message for errors
// that surface through the SendMulti batch result, where we cannot return
// HTTP status codes per-item but still want to avoid leaking internal details.
func sanitizeNotificationError(err error) string {
	if errors.Is(err, domain.ErrNotFound) {
		return "channel config not found"
	}
	if errors.Is(err, domain.ErrValidationFailed) {
		return "validation failed"
	}
	if errors.Is(err, domain.ErrUnsupportedChannel) {
		return "unsupported channel"
	}
	return "internal server error"
}
