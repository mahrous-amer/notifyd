package handler

import (
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/bse/notifyd/internal/auth"
	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/pkg/response"
)

type AuthHandler struct {
	tenantRepo domain.TenantRepository
	jwtMgr     *auth.JWTManager
}

func NewAuthHandler(tenantRepo domain.TenantRepository, jwtMgr *auth.JWTManager) *AuthHandler {
	return &AuthHandler{tenantRepo: tenantRepo, jwtMgr: jwtMgr}
}

type tokenRequest struct {
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn string `json:"expires_in"`
}

func (h *AuthHandler) IssueToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.APIKey == "" || req.APISecret == "" {
		response.Error(w, http.StatusBadRequest, "api_key and api_secret are required")
		return
	}

	tenant, err := h.tenantRepo.GetByAPIKey(r.Context(), req.APIKey)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if !tenant.IsActive {
		response.Error(w, http.StatusForbidden, "tenant is disabled")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(tenant.APISecret), []byte(req.APISecret)); err != nil {
		response.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := h.jwtMgr.GenerateToken(tenant.ID, tenant.Slug)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	response.JSON(w, http.StatusOK, tokenResponse{
		Token:     token,
		ExpiresIn: h.jwtMgr.Expiration().String(),
	})
}
