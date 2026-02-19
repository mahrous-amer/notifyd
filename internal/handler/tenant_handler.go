package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/service"
	"github.com/bse/notifyd/pkg/response"
)

type TenantHandler struct {
	svc *service.TenantService
}

func NewTenantHandler(svc *service.TenantService) *TenantHandler {
	return &TenantHandler{svc: svc}
}

func (h *TenantHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input domain.CreateTenantInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.svc.Create(r.Context(), input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	response.JSON(w, http.StatusCreated, result)
}

func (h *TenantHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	tenant, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, tenant)
}

func (h *TenantHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	var input domain.UpdateTenantInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tenant, err := h.svc.Update(r.Context(), id, input)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, tenant)
}

func (h *TenantHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "tenantID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	if err := h.svc.Delete(r.Context(), id); err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *TenantHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	tenants, total, err := h.svc.List(r.Context(), limit, offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response.JSON(w, http.StatusOK, response.ListResponse{
		Data:   tenants,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}
