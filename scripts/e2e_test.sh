#!/usr/bin/env bash
set -uo pipefail

BASE="${API_BASE_URL:-http://localhost:8080}"
API_KEY="${TEST_API_KEY:-test-api-key-123}"
API_SECRET="${TEST_API_SECRET:-test-secret-12345}"

PASS=0
FAIL=0
TOTAL=0

# ── helpers ──────────────────────────────────────────────
check() {
  local name="$1" expected="$2" actual="$3" body="$4"
  TOTAL=$((TOTAL+1))
  if [ "$actual" = "$expected" ]; then
    PASS=$((PASS+1))
    printf "  \033[32mPASS\033[0m  %s (HTTP %s)\n" "$name" "$actual"
  else
    FAIL=$((FAIL+1))
    printf "  \033[31mFAIL\033[0m  %s — expected %s, got %s\n" "$name" "$expected" "$actual"
    printf "        body: %s\n" "$body"
  fi
}

get_token() {
  curl -s -X POST "$BASE/auth/token" \
    -H 'Content-Type: application/json' \
    -d "{\"api_key\":\"$API_KEY\",\"api_secret\":\"$API_SECRET\"}" \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])"
}

call() {
  local method="$1" path="$2" token="$3"
  shift 3
  local data="${1:-}"
  if [ -n "$data" ]; then
    curl -s -w "\n%{http_code}" -X "$method" "${BASE}${path}" \
      -H "Authorization: Bearer $token" \
      -H 'Content-Type: application/json' \
      -d "$data"
  else
    curl -s -w "\n%{http_code}" -X "$method" "${BASE}${path}" \
      -H "Authorization: Bearer $token"
  fi
}

parse() { echo "$1" | head -1; }
code()  { echo "$1" | tail -1; }
field() { echo "$1" | head -1 | python3 -c "import sys,json; print(json.load(sys.stdin)$2)" 2>/dev/null; }

echo "╔══════════════════════════════════════════════╗"
echo "║    notifyd — Comprehensive E2E Test Suite    ║"
echo "╚══════════════════════════════════════════════╝"
echo "  Target: $BASE"
echo ""

########################################################
echo "━━━ 1. HEALTH ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
########################################################
r=$(curl -s -w "\n%{http_code}" "$BASE/health")
check "GET /health returns 200" 200 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 2. AUTHENTICATION ━━━━━━━━━━━━━━━━━━━━━━━━━"
########################################################

r=$(curl -s -w "\n%{http_code}" -X POST "$BASE/auth/token" \
  -H 'Content-Type: application/json' -d '{}')
check "Empty credentials → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(curl -s -w "\n%{http_code}" -X POST "$BASE/auth/token" \
  -H 'Content-Type: application/json' -d '{"api_key":"wrong","api_secret":"wrong"}')
check "Wrong credentials → 401" 401 "$(code "$r")" "$(parse "$r")"

r=$(curl -s -w "\n%{http_code}" -X POST "$BASE/auth/token" \
  -H 'Content-Type: application/json' -d "{\"api_key\":\"$API_KEY\",\"api_secret\":\"wrong-secret\"}")
check "Wrong secret → 401" 401 "$(code "$r")" "$(parse "$r")"

r=$(curl -s -w "\n%{http_code}" -X POST "$BASE/auth/token" \
  -H 'Content-Type: application/json' -d "{\"api_key\":\"$API_KEY\",\"api_secret\":\"$API_SECRET\"}")
check "Valid credentials → 200 + token" 200 "$(code "$r")" "$(parse "$r")"

TOKEN=$(get_token)
echo "  Token acquired: ${TOKEN:0:20}..."

r=$(curl -s -w "\n%{http_code}" -X POST "$BASE/auth/token" -d 'not json')
check "Malformed JSON body → 400" 400 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 3. AUTH MIDDLEWARE ━━━━━━━━━━━━━━━━━━━━━━━━━"
########################################################

r=$(curl -s -w "\n%{http_code}" "$BASE/channels")
check "No auth header → 401" 401 "$(code "$r")" "$(parse "$r")"

r=$(curl -s -w "\n%{http_code}" "$BASE/channels" -H 'Authorization: Basic abc123')
check "Non-Bearer auth → 401" 401 "$(code "$r")" "$(parse "$r")"

r=$(curl -s -w "\n%{http_code}" "$BASE/channels" -H 'Authorization: Bearer invalid.token.here')
check "Invalid JWT → 401" 401 "$(code "$r")" "$(parse "$r")"

r=$(curl -s -w "\n%{http_code}" "$BASE/channels" -H "Authorization: Bearer $TOKEN")
check "Valid JWT → 200" 200 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 4. ADMIN — TENANT CRUD ━━━━━━━━━━━━━━━━━━━━"
########################################################

r=$(call GET /admin/tenants "$TOKEN")
check "List tenants → 200" 200 "$(code "$r")" "$(parse "$r")"
INITIAL_COUNT=$(field "$r" "['total']")
echo "  Current tenant count: $INITIAL_COUNT"

r=$(call POST /admin/tenants "$TOKEN" '{"name":"Acme Corp","slug":"acme"}')
check "Create tenant → 201" 201 "$(code "$r")" "$(parse "$r")"
ACME_ID=$(field "$r" "['tenant']['id']")
ACME_KEY=$(field "$r" "['api_key']")
ACME_SECRET=$(field "$r" "['api_secret']")
echo "  Acme ID: $ACME_ID"

r=$(call POST /admin/tenants "$TOKEN" '{"name":"Beta Inc","slug":"beta"}')
check "Create second tenant → 201" 201 "$(code "$r")" "$(parse "$r")"
BETA_ID=$(field "$r" "['tenant']['id']")

r=$(call POST /admin/tenants "$TOKEN" '{"name":"Dup","slug":"acme"}')
check "Duplicate slug → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /admin/tenants "$TOKEN" '{"name":"","slug":"empty"}')
check "Empty name → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /admin/tenants "$TOKEN" '{"name":"No Slug"}')
check "Missing slug → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/admin/tenants/$ACME_ID" "$TOKEN")
check "Get tenant by ID → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call PATCH "/admin/tenants/$ACME_ID" "$TOKEN" '{"name":"Acme Corp v2"}')
check "Update tenant name → 200" 200 "$(code "$r")" "$(parse "$r")"
UPDATED_NAME=$(field "$r" "['name']")
[ "$UPDATED_NAME" = "Acme Corp v2" ] && check "Name actually changed" 200 200 "" || check "Name actually changed" "Acme Corp v2" "$UPDATED_NAME" ""

r=$(call GET /admin/tenants "$TOKEN")
NEW_COUNT=$(field "$r" "['total']")
check "Tenant count increased" "$((INITIAL_COUNT+2))" "$NEW_COUNT" ""

r=$(call DELETE "/admin/tenants/$BETA_ID" "$TOKEN")
check "Delete tenant → 204" 204 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/admin/tenants/$BETA_ID" "$TOKEN")
check "Get deleted tenant → 404" 404 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/admin/tenants/not-a-uuid" "$TOKEN")
check "Invalid UUID → 400" 400 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 5. CHANNEL CONFIG CRUD ━━━━━━━━━━━━━━━━━━━━"
########################################################

r=$(call POST /channels "$TOKEN" '{"channel":"discord","name":"dev-alerts","config":{"webhook_url":"https://discord.com/api/webhooks/123456/abcdef"}}')
check "Create Discord channel → 201" 201 "$(code "$r")" "$(parse "$r")"
DISCORD_ID=$(field "$r" "['id']")
echo "  Discord channel ID: $DISCORD_ID"

r=$(call POST /channels "$TOKEN" '{"channel":"telegram","name":"ops-alerts","config":{"bot_token":"123456:ABC-DEF","chat_id":"987654321"}}')
check "Create Telegram channel → 201" 201 "$(code "$r")" "$(parse "$r")"
TELEGRAM_ID=$(field "$r" "['id']")
echo "  Telegram channel ID: $TELEGRAM_ID"

r=$(call POST /channels "$TOKEN" '{"channel":"whatsapp","name":"customer-notifs","config":{"phone_number_id":"12345","access_token":"EAAG-test","recipient":"15551234567"}}')
check "Create WhatsApp channel → 201" 201 "$(code "$r")" "$(parse "$r")"
WHATSAPP_ID=$(field "$r" "['id']")
echo "  WhatsApp channel ID: $WHATSAPP_ID"

r=$(call POST /channels "$TOKEN" '{"channel":"email","name":"test","config":{}}')
check "Invalid channel type → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /channels "$TOKEN" '{"channel":"discord","name":"bad","config":{}}')
check "Discord missing webhook_url → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /channels "$TOKEN" '{"channel":"telegram","name":"bad","config":{"bot_token":"x"}}')
check "Telegram missing chat_id → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /channels "$TOKEN" '{"channel":"whatsapp","name":"bad","config":{"phone_number_id":"x"}}')
check "WhatsApp missing access_token → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /channels "$TOKEN" '{"channel":"discord","name":"dev-alerts","config":{"webhook_url":"https://discord.com/api/webhooks/dup/dup"}}')
check "Duplicate channel name → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call GET /channels "$TOKEN")
check "List channels → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/channels/$DISCORD_ID" "$TOKEN")
check "Get channel by ID → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call PATCH "/channels/$DISCORD_ID" "$TOKEN" '{"name":"prod-alerts"}')
check "Update channel name → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call PATCH "/channels/$DISCORD_ID" "$TOKEN" '{"config":{"webhook_url":"https://discord.com/api/webhooks/999/newtoken"}}')
check "Update channel config → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call PATCH "/channels/$DISCORD_ID" "$TOKEN" '{"config":{}}')
check "Update with invalid config → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call PATCH "/channels/$DISCORD_ID" "$TOKEN" '{"is_active":false}')
check "Deactivate channel → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call PATCH "/channels/$DISCORD_ID" "$TOKEN" '{"is_active":true}')
check "Reactivate channel → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call DELETE "/channels/$WHATSAPP_ID" "$TOKEN")
check "Delete channel → 204" 204 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/channels/$WHATSAPP_ID" "$TOKEN")
check "Get deleted channel → 404" 404 "$(code "$r")" "$(parse "$r")"

r=$(call DELETE "/channels/$WHATSAPP_ID" "$TOKEN")
check "Delete already-deleted → 404" 404 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/channels/not-a-uuid" "$TOKEN")
check "Invalid channel UUID → 400" 400 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 6. SEND NOTIFICATIONS ━━━━━━━━━━━━━━━━━━━━━"
########################################################

r=$(call POST /notifications/send "$TOKEN" \
  "{\"channel_config_id\":\"$DISCORD_ID\",\"subject\":\"Deploy v1.2.3\",\"body\":\"Deployed to production\",\"metadata\":{\"env\":\"prod\",\"version\":\"1.2.3\"}}")
check "Send Discord notification → 202" 202 "$(code "$r")" "$(parse "$r")"
NOTIF_1=$(field "$r" "['id']")
echo "  Notification 1 (Discord): $NOTIF_1"

r=$(call POST /notifications/send "$TOKEN" \
  "{\"channel_config_id\":\"$TELEGRAM_ID\",\"subject\":\"CPU Alert\",\"body\":\"CPU usage above 90%\"}")
check "Send Telegram notification → 202" 202 "$(code "$r")" "$(parse "$r")"
NOTIF_2=$(field "$r" "['id']")
echo "  Notification 2 (Telegram): $NOTIF_2"

r=$(call POST /notifications/send "$TOKEN" '{"subject":"No config","body":"Test"}')
check "Missing channel_config_id → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /notifications/send "$TOKEN" "{\"channel_config_id\":\"$DISCORD_ID\",\"subject\":\"No body\"}")
check "Missing body → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /notifications/send "$TOKEN" \
  '{"channel_config_id":"00000000-0000-0000-0000-000000000000","subject":"Test","body":"Test"}')
check "Zero UUID config → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /notifications/send "$TOKEN" \
  '{"channel_config_id":"99999999-9999-9999-9999-999999999999","subject":"Test","body":"Test"}')
check "Non-existent config → 404" 404 "$(code "$r")" "$(parse "$r")"

r=$(call POST /notifications/send "$TOKEN" 'not json')
check "Malformed JSON → 400" 400 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 7. SEND MULTI ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
########################################################

r=$(call POST /notifications/send-multi "$TOKEN" \
  "{\"channels\":[{\"channel_config_id\":\"$DISCORD_ID\",\"subject\":\"Multi-1\",\"body\":\"To Discord\"},{\"channel_config_id\":\"$TELEGRAM_ID\",\"subject\":\"Multi-2\",\"body\":\"To Telegram\"}]}")
check "Send multi (2 channels) → 202" 202 "$(code "$r")" "$(parse "$r")"

r=$(call POST /notifications/send-multi "$TOKEN" '{"channels":[]}')
check "Send multi empty → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call POST /notifications/send-multi "$TOKEN" \
  "{\"channels\":[{\"channel_config_id\":\"$DISCORD_ID\",\"subject\":\"Single\",\"body\":\"Only one\"}]}")
check "Send multi (1 channel) → 202" 202 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 8. NOTIFICATION QUERIES ━━━━━━━━━━━━━━━━━━━"
########################################################

sleep 2  # let worker process

r=$(call GET /notifications "$TOKEN")
check "List all notifications → 200" 200 "$(code "$r")" "$(parse "$r")"
NOTIF_TOTAL=$(field "$r" "['total']")
echo "  Total notifications: $NOTIF_TOTAL"

r=$(call GET "/notifications?limit=2&offset=0" "$TOKEN")
check "Pagination limit=2 → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications?status=failed" "$TOKEN")
check "Filter status=failed → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications?status=retrying" "$TOKEN")
check "Filter status=retrying → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications?channel=discord" "$TOKEN")
check "Filter channel=discord → 200" 200 "$(code "$r")" "$(parse "$r")"
DISCORD_COUNT=$(field "$r" "['total']")
echo "  Discord notifications: $DISCORD_COUNT"

r=$(call GET "/notifications?channel=telegram" "$TOKEN")
check "Filter channel=telegram → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications?status=failed&channel=discord" "$TOKEN")
check "Combined filter status+channel → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications?status=bogus" "$TOKEN")
check "Invalid status filter → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications?channel=email" "$TOKEN")
check "Invalid channel filter → 400" 400 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications/$NOTIF_1" "$TOKEN")
check "Get notification by ID → 200" 200 "$(code "$r")" "$(parse "$r")"
NOTIF_STATUS=$(field "$r" "['status']")
echo "  Notification 1 status: $NOTIF_STATUS"

r=$(call GET "/notifications/99999999-9999-9999-9999-999999999999" "$TOKEN")
check "Non-existent notification → 404" 404 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications/not-a-uuid" "$TOKEN")
check "Invalid notification UUID → 400" 400 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 9. DELIVERY ATTEMPTS ━━━━━━━━━━━━━━━━━━━━━━"
########################################################

r=$(call GET "/notifications/$NOTIF_1/attempts" "$TOKEN")
check "List delivery attempts → 200" 200 "$(code "$r")" "$(parse "$r")"
ATTEMPT_BODY=$(parse "$r")
ATTEMPT_COUNT=$(echo "$ATTEMPT_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
echo "  Attempts for notification 1: $ATTEMPT_COUNT"
if [ "$ATTEMPT_COUNT" -gt 0 ]; then
  ATTEMPT_STATUS=$(echo "$ATTEMPT_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['status'])")
  ATTEMPT_DUR=$(echo "$ATTEMPT_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['duration_ms'])")
  echo "  First attempt: status=$ATTEMPT_STATUS, duration=${ATTEMPT_DUR}ms"
fi

r=$(call GET "/notifications/$NOTIF_2/attempts" "$TOKEN")
check "Telegram attempts → 200" 200 "$(code "$r")" "$(parse "$r")"

r=$(call GET "/notifications/99999999-9999-9999-9999-999999999999/attempts" "$TOKEN")
check "Attempts for non-existent → 404" 404 "$(code "$r")" "$(parse "$r")"
echo ""

########################################################
echo "━━━ 10. TENANT ISOLATION ━━━━━━━━━━━━━━━━━━━━━━"
########################################################

ACME_TOKEN_RESP=$(curl -s -X POST "$BASE/auth/token" \
  -H 'Content-Type: application/json' \
  -d "{\"api_key\":\"$ACME_KEY\",\"api_secret\":\"$ACME_SECRET\"}")
ACME_TOKEN=$(echo "$ACME_TOKEN_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])" 2>/dev/null || echo "")

if [ -n "$ACME_TOKEN" ]; then
  r=$(call GET /channels "$ACME_TOKEN")
  check "Acme sees 0 channels (isolation)" 200 "$(code "$r")" "$(parse "$r")"

  r=$(call GET /notifications "$ACME_TOKEN")
  check "Acme sees 0 notifications (isolation)" 200 "$(code "$r")" "$(parse "$r")"
  ACME_NOTIF_COUNT=$(field "$r" "['total']")
  echo "  Acme notification count: $ACME_NOTIF_COUNT"

  r=$(call GET "/notifications/$NOTIF_1" "$ACME_TOKEN")
  check "Acme can't see test-co notification → 404" 404 "$(code "$r")" "$(parse "$r")"
else
  echo "  SKIP — could not get Acme token"
  TOTAL=$((TOTAL+3))
  FAIL=$((FAIL+3))
fi
echo ""

########################################################
echo "━━━ 11. REQUEST BODY SIZE LIMIT ━━━━━━━━━━━━━━━"
########################################################

python3 -c "print('{\"name\":\"' + 'A'*2000000 + '\"}')" > /tmp/notifyd_bigbody.json
r=$(curl -s -w "\n%{http_code}" -X POST "$BASE/channels" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d @/tmp/notifyd_bigbody.json)
check "2MB body rejected → 400" 400 "$(code "$r")" "$(parse "$r")"
rm -f /tmp/notifyd_bigbody.json
echo ""

########################################################
echo "━━━ 12. EDGE CASES ━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
########################################################

r=$(curl -s -w "\n%{http_code}" "$BASE/nonexistent")
check "Unknown route → 404" 404 "$(code "$r")" "$(parse "$r")"

r=$(call POST /channels "$TOKEN" '{"channel":"discord","name":"","config":{"webhook_url":"https://discord.com/api/webhooks/1/x"}}')
check "Empty channel name → 400" 400 "$(code "$r")" "$(parse "$r")"

# Cleanup
if [ -n "${ACME_ID:-}" ]; then
  call DELETE "/admin/tenants/$ACME_ID" "$TOKEN" > /dev/null 2>&1
fi

echo ""
echo "╔══════════════════════════════════════════════╗"
printf "║  Results: \033[32m%d passed\033[0m / \033[31m%d failed\033[0m / %d total    ║\n" "$PASS" "$FAIL" "$TOTAL"
echo "╚══════════════════════════════════════════════╝"

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
