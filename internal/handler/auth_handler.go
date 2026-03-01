package handler

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/auth"
	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/pkg/response"
)

type AuthHandler struct {
	tenantRepo     domain.TenantRepository
	jwtMgr         *auth.JWTManager
	adminAPIKey    string
	adminAPISecret string
}

func NewAuthHandler(
	tenantRepo domain.TenantRepository,
	jwtMgr *auth.JWTManager,
	adminAPIKey string,
	adminAPISecret string,
) *AuthHandler {
	return &AuthHandler{
		tenantRepo:     tenantRepo,
		jwtMgr:         jwtMgr,
		adminAPIKey:    adminAPIKey,
		adminAPISecret: adminAPISecret,
	}
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

	if h.adminAPIKey != "" && subtle.ConstantTimeCompare([]byte(req.APIKey), []byte(h.adminAPIKey)) == 1 {
		h.issueAdminToken(w, req.APISecret)
		return
	}

	h.issueTenantToken(w, r, req)
}

func (h *AuthHandler) issueAdminToken(w http.ResponseWriter, providedSecret string) {
	if subtle.ConstantTimeCompare([]byte(providedSecret), []byte(h.adminAPISecret)) != 1 {
		response.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Admin tokens use uuid.Nil as the tenant ID since admins have cross-tenant access.
	token, err := h.jwtMgr.GenerateToken(uuid.Nil, "", true)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	response.JSON(w, http.StatusOK, tokenResponse{
		Token:     token,
		ExpiresIn: h.jwtMgr.Expiration().String(),
	})
}

func (h *AuthHandler) issueTenantToken(w http.ResponseWriter, r *http.Request, req tokenRequest) {
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

	token, err := h.jwtMgr.GenerateToken(tenant.ID, tenant.Slug, false)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	response.JSON(w, http.StatusOK, tokenResponse{
		Token:     token,
		ExpiresIn: h.jwtMgr.Expiration().String(),
	})
}
