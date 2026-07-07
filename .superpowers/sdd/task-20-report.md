# Task 20 Report — fix: refund quota on rejected sends and remove dead auth/gate code

## Fix Status

| Fix | Status |
|-----|--------|
| Fix 1: Refund quota on rejected sends (TDD) | DONE |
| Fix 3: Remove dead GetByAPIKey from TenantRepository | DONE |
| Fix 4: Remove dead channel Update plan-gate branch | DONE |

---

## Fix 1: Refund Quota on Rejected Sends

### a) `quota.Service.Refund` added to `internal/quota/quota.go`
- Resolves entitlements, derives the usage key, and calls `DecrBy(ctx, key, n)`.
- Guards against underflow: if `newVal < 0`, resets key to `0`.
- Uses zerolog debug-level logging consistent with the rest of the file.

### b) `internal/quota/middleware.go` updated
- Interface renamed from `Reserver` to `ReserverRefunder`; `Refund` added to it.
- `statusRecorder` struct wraps `http.ResponseWriter` to capture the HTTP status written by the downstream handler (defaults to `200` if `WriteHeader` is never called).
- After `next.ServeHTTP(recorder, r)`, if `recorder.status >= 400` the middleware calls `svc.Refund(ctx, claims.TenantID, n)`.
- Added comment: `// NOTE: threshold webhooks fired during Reserve before this reject was known; a refunded reject may have already emitted a webhook. Acceptable while billing is not consuming webhooks.`

### c) TDD evidence — `internal/quota/middleware_test.go`

Three new test cases were added on top of the existing three (all pass with `-race`):

| Test | What it verifies |
|------|-----------------|
| `TestQuotaMiddleware_Refund_On4xx` | Handler returns 400 → `Refund` called once with the reserved `n=1` |
| `TestQuotaMiddleware_NoRefund_On2xx` | Handler returns 202 → `Refund` NOT called (`refundCalls` empty) |
| `TestQuotaMiddleware_Refund_SendMulti_On4xx` | send-multi with `n=3`, handler returns 403 → `Refund` called with `3` |

The `stubReserver` struct was updated to implement `ReserverRefunder` (added `Refund` method) and records all refund calls in `refundCalls []int64`.

---

## Fix 3: Remove Dead `GetByAPIKey` from `TenantRepository`

### Files changed
- `internal/domain/tenant.go` — removed `GetByAPIKey` from the `TenantRepository` interface
- `internal/repository/pg_tenant.go` — removed the `GetByAPIKey` method implementation
- `internal/domain/mocks/mock_tenant.go` — removed both the mock method and its recorder method
- `internal/handler/auth_handler_test.go` — removed `getByAPIKeyFn` field and `GetByAPIKey` method from `stubTenantRepo`
- `internal/worker/retention_test.go` — removed `GetByAPIKey` from `fakeMaintTenantRepo`

### Grep proof

```
$ grep -rn "GetByAPIKey" /Users/mahrous/Projects/notifyd/internal/
internal/handler/auth_handler.go:91:        key, err := h.keyRepo.GetByAPIKey(r.Context(), req.APIKey)
internal/handler/auth_handler_test.go:42:   func (s *stubAPIKeyRepo) GetByAPIKey(...) (*domain.APIKey, error)
internal/handler/auth_handler_test.go:212:  t.Fatal("GetByAPIKey must not be called for admin authentication")
internal/repository/pg_apikey_repo.go:30:   func (r *PgAPIKeyRepo) GetByAPIKey(...)
internal/service/apikey_service_test.go:42: func (f *fakeAPIKeyRepo) GetByAPIKey(...)
internal/domain/apikey.go:25:              GetByAPIKey(ctx context.Context, apiKey string) (*APIKey, error)
```

All six remaining references are on `APIKeyRepository` / `domain.APIKey`. Zero references remain on `TenantRepository`.

---

## Fix 4: Remove Dead Channel Update Plan-Gate Branch

### Files changed
- `internal/handler/channel_handler.go` — removed the `domain.ErrChannelNotInPlan → 403` case from `Update` (kept in `Create`)
- `docs/openapi.yaml` — removed the `403 CHANNEL_NOT_IN_PLAN` response from `PATCH /channels/{channelID}` only (kept on `POST /channels` and `POST /notifications/send`)

---

## Files Changed

```
internal/quota/quota.go
internal/quota/middleware.go
internal/quota/middleware_test.go
internal/domain/tenant.go
internal/repository/pg_tenant.go
internal/domain/mocks/mock_tenant.go
internal/handler/auth_handler_test.go
internal/worker/retention_test.go
internal/handler/channel_handler.go
docs/openapi.yaml
.superpowers/sdd/task-20-report.md
```

---

## Test Output Summary

```
ok  github.com/bse/notifyd/internal/auth      1.547s
ok  github.com/bse/notifyd/internal/bot       2.312s
ok  github.com/bse/notifyd/internal/domain    1.900s
ok  github.com/bse/notifyd/internal/handler   2.607s
ok  github.com/bse/notifyd/internal/provider  2.793s
ok  github.com/bse/notifyd/internal/quota     3.176s
ok  github.com/bse/notifyd/internal/service   9.399s
ok  github.com/bse/notifyd/internal/worker    3.610s
ok  github.com/bse/notifyd/pkg/response       3.962s
```

All packages pass with `-race -count=1`. No failures.

---

## Build / Lint Output

- `go build ./...` — clean (no output)
- `go vet ./...` — clean (no output)
- `npx @redocly/cli lint docs/openapi.yaml` — **"Your API description is valid."** 2 pre-existing warnings (info-license, operation-4xx-response on /health) unrelated to these changes.

---

## Concerns

None. All removals were confirmed dead code; no callers of `TenantRepository.GetByAPIKey` exist through the interface. The underflow guard in `Refund` is a defensive no-op in normal operation.
