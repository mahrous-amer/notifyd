package notifyd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer builds an httptest server that issues token
// "test-token-<n>" on each /auth/token call (n increments per call, so
// tests can distinguish a cached token from a freshly-issued one) and
// dispatches every other path through handler. handler receives the
// Authorization header's bearer token so tests can assert on it. The
// returned tokenCallCount function is safe to call concurrently with
// in-flight requests.
func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, bearerToken string)) (server *httptest.Server, client *Client, tokenCallCount func() int64) {
	t.Helper()

	var callCount int64
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&callCount, 1)
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("token request body: %v", err)
		}
		if req["api_key"] != "test-key" || req["api_secret"] != "test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(APIError{Error: "INVALID_CREDENTIALS"})
			return
		}
		json.NewEncoder(w).Encode(tokenResponse{
			Token:     fmt.Sprintf("test-token-%d", n),
			ExpiresIn: "24h0m0s",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		token := ""
		if len(auth) > len(prefix) {
			token = auth[len(prefix):]
		}
		handler(w, r, token)
	})

	testServer := httptest.NewServer(mux)
	t.Cleanup(testServer.Close)

	newClient, err := New(Config{
		APIKey:    "test-key",
		APISecret: "test-secret",
		BaseURL:   testServer.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return testServer, newClient, func() int64 { return atomic.LoadInt64(&callCount) }
}

func TestTokenExchangeAndCaching(t *testing.T) {
	ctx := context.Background()
	var channelsCalls int
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		if r.URL.Path != "/channels" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		channelsCalls++
		if token != "test-token-1" {
			t.Errorf("call %d: got token %q, want test-token-1 (should reuse cached token)", channelsCalls, token)
		}
		json.NewEncoder(w).Encode([]ChannelConfig{})
	})

	if _, err := client.ListChannels(ctx); err != nil {
		t.Fatalf("first ListChannels: %v", err)
	}
	if _, err := client.ListChannels(ctx); err != nil {
		t.Fatalf("second ListChannels: %v", err)
	}
	if channelsCalls != 2 {
		t.Fatalf("got %d channels calls, want 2", channelsCalls)
	}
}

func TestRefreshOnceOn401(t *testing.T) {
	ctx := context.Background()
	var requestTokens []string
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		requestTokens = append(requestTokens, token)
		// Reject the first token to simulate an expired/revoked token the
		// client didn't know about yet; accept any subsequent token.
		if token == "test-token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(APIError{Error: "TOKEN_EXPIRED"})
			return
		}
		json.NewEncoder(w).Encode([]ChannelConfig{})
	})

	if _, err := client.ListChannels(ctx); err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(requestTokens) != 2 {
		t.Fatalf("got %d requests, want 2 (initial + one retry)", len(requestTokens))
	}
	if requestTokens[0] != "test-token-1" || requestTokens[1] != "test-token-2" {
		t.Fatalf("got tokens %v, want [test-token-1 test-token-2]", requestTokens)
	}
}

func TestRefreshOnlyOnceNotLooped(t *testing.T) {
	ctx := context.Background()
	var attempts int
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		attempts++
		// Every attempt returns 401, simulating persistently bad
		// credentials. The client must give up after one retry, not loop.
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(APIError{Error: "INVALID_TOKEN"})
	})

	_, err := client.ListChannels(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	reqErr, ok := err.(*RequestError)
	if !ok {
		t.Fatalf("got error type %T, want *RequestError", err)
	}
	if reqErr.StatusCode != http.StatusUnauthorized || reqErr.Code != "INVALID_TOKEN" {
		t.Fatalf("got %+v, want 401 INVALID_TOKEN", reqErr)
	}
	if attempts != 2 {
		t.Fatalf("got %d attempts, want 2 (initial + one retry, no loop)", attempts)
	}
}

func TestSend(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		if r.URL.Path != "/notifications/send" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body SendInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.ChannelConfigID != "chan-1" || body.Body != "hello" {
			t.Fatalf("unexpected body: %+v", body)
		}
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(Notification{ID: "notif-1", Status: StatusPending})
	})

	notification, err := client.Send(ctx, SendInput{ChannelConfigID: "chan-1", Body: "hello"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if notification.ID != "notif-1" || notification.Status != StatusPending {
		t.Fatalf("got %+v", notification)
	}
}

func TestSend_QuotaExceededExposesUpgradeURL(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error":       "QUOTA_EXCEEDED",
			"upgrade_url": "https://notifyd.fluxintek.com/account/upgrade",
		})
	})

	_, err := client.Send(ctx, SendInput{ChannelConfigID: "chan-1", Body: "hello"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	reqErr, ok := err.(*RequestError)
	if !ok {
		t.Fatalf("got error type %T, want *RequestError", err)
	}
	if reqErr.StatusCode != http.StatusTooManyRequests || reqErr.Code != "QUOTA_EXCEEDED" {
		t.Fatalf("got %+v, want 429 QUOTA_EXCEEDED", reqErr)
	}

	upgradeURL, ok := reqErr.Field("upgrade_url")
	if !ok {
		t.Fatalf("expected upgrade_url field to be present in %+v", reqErr.Body)
	}
	if upgradeURL != "https://notifyd.fluxintek.com/account/upgrade" {
		t.Fatalf("got upgrade_url %q, want the account upgrade link", upgradeURL)
	}
}

func TestSendMulti(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		if r.URL.Path != "/notifications/send-multi" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(SendMultiResponse{
			Sent:   []Notification{{ID: "notif-1"}},
			Errors: []string{"chan-2: CHANNEL_NOT_FOUND"},
		})
	})

	result, err := client.SendMulti(ctx, []SendInput{
		{ChannelConfigID: "chan-1", Body: "a"},
		{ChannelConfigID: "chan-2", Body: "b"},
	})
	if err != nil {
		t.Fatalf("SendMulti: %v", err)
	}
	if len(result.Sent) != 1 || len(result.Errors) != 1 {
		t.Fatalf("got %+v", result)
	}
}

func TestListNotifications(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		if r.URL.Path != "/notifications" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "delivered" {
			t.Fatalf("got status filter %q, want delivered", got)
		}
		json.NewEncoder(w).Encode(NotificationList{
			Data:  []Notification{{ID: "notif-1", Status: StatusDelivered}},
			Total: 1, Limit: 20, Offset: 0,
		})
	})

	list, err := client.ListNotifications(ctx, ListNotificationsParams{Status: StatusDelivered})
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if list.Total != 1 || len(list.Data) != 1 {
		t.Fatalf("got %+v", list)
	}
}

func TestGetNotification(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		if r.URL.Path != "/notifications/notif-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Notification{ID: "notif-1", Status: StatusDelivered})
	})

	notification, err := client.GetNotification(ctx, "notif-1")
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if notification.Status != StatusDelivered {
		t.Fatalf("got %+v", notification)
	}
}

func TestListAttempts(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		if r.URL.Path != "/notifications/notif-1/attempts" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]DeliveryAttempt{{ID: "attempt-1", AttemptNumber: 1, Status: AttemptSuccess}})
	})

	attempts, err := client.ListAttempts(ctx, "notif-1")
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 1 || attempts[0].Status != AttemptSuccess {
		t.Fatalf("got %+v", attempts)
	}
}

func TestChannelsCRUD(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/channels":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(ChannelConfig{ID: "chan-1", Channel: ChannelTelegram})
		case r.Method == http.MethodGet && r.URL.Path == "/channels/chan-1":
			json.NewEncoder(w).Encode(ChannelConfig{ID: "chan-1", Channel: ChannelTelegram})
		case r.Method == http.MethodPatch && r.URL.Path == "/channels/chan-1":
			json.NewEncoder(w).Encode(ChannelConfig{ID: "chan-1", Channel: ChannelTelegram, IsActive: false})
		case r.Method == http.MethodDelete && r.URL.Path == "/channels/chan-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	created, err := client.CreateChannel(ctx, CreateChannelInput{
		Channel: ChannelTelegram,
		Name:    "ops-alerts",
		Config:  map[string]any{"bot_token": "x", "chat_id": "y"},
	})
	if err != nil || created.ID != "chan-1" {
		t.Fatalf("CreateChannel: %+v, %v", created, err)
	}

	fetched, err := client.GetChannel(ctx, "chan-1")
	if err != nil || fetched.ID != "chan-1" {
		t.Fatalf("GetChannel: %+v, %v", fetched, err)
	}

	inactive := false
	updated, err := client.UpdateChannel(ctx, "chan-1", UpdateChannelInput{IsActive: &inactive})
	if err != nil || updated.IsActive {
		t.Fatalf("UpdateChannel: %+v, %v", updated, err)
	}

	if err := client.DeleteChannel(ctx, "chan-1"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
}

func TestKeysCRUD(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/keys":
			json.NewEncoder(w).Encode([]APIKey{{ID: "key-1", Label: "ci"}})
		case r.Method == http.MethodPost && r.URL.Path == "/keys":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(CreateAPIKeyResponse{
				Key:       APIKey{ID: "key-2", Label: "new"},
				APISecret: "shown-once",
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/keys/key-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	keys, err := client.ListAPIKeys(ctx)
	if err != nil || len(keys) != 1 {
		t.Fatalf("ListAPIKeys: %+v, %v", keys, err)
	}

	created, err := client.CreateAPIKey(ctx, "new")
	if err != nil || created.APISecret != "shown-once" {
		t.Fatalf("CreateAPIKey: %+v, %v", created, err)
	}

	if err := client.RevokeAPIKey(ctx, "key-1"); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
}

func TestWebhooksCRUD(t *testing.T) {
	ctx := context.Background()
	_, client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/webhooks":
			json.NewEncoder(w).Encode([]WebhookEndpoint{{ID: "wh-1"}})
		case r.Method == http.MethodPost && r.URL.Path == "/webhooks":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(WebhookEndpointCreated{
				WebhookEndpoint: WebhookEndpoint{ID: "wh-2", URL: "https://example.com/hook"},
				Secret:          "whsec_shown_once",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/webhooks/wh-1":
			active := false
			json.NewEncoder(w).Encode(WebhookEndpoint{ID: "wh-1", IsActive: active})
		case r.Method == http.MethodDelete && r.URL.Path == "/webhooks/wh-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	webhooks, err := client.ListWebhooks(ctx)
	if err != nil || len(webhooks) != 1 {
		t.Fatalf("ListWebhooks: %+v, %v", webhooks, err)
	}

	created, err := client.CreateWebhook(ctx, CreateWebhookInput{
		URL:    "https://example.com/hook",
		Events: []WebhookEventType{EventNotificationDelivered},
	})
	if err != nil || created.Secret != "whsec_shown_once" {
		t.Fatalf("CreateWebhook: %+v, %v", created, err)
	}

	inactive := false
	updated, err := client.UpdateWebhook(ctx, "wh-1", UpdateWebhookInput{IsActive: &inactive})
	if err != nil || updated.IsActive {
		t.Fatalf("UpdateWebhook: %+v, %v", updated, err)
	}

	if err := client.DeleteWebhook(ctx, "wh-1"); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for missing credentials")
	}
	if _, err := New(Config{APIKey: "k"}); err == nil {
		t.Fatal("expected error for missing api secret")
	}
}

// TestConcurrentCallersShareOneTokenExchange proves the tokenMu
// hold-lock-across-I/O design in authenticatedToken (see the comment
// there): many callers racing to make their first authenticated request
// must still result in exactly one /auth/token exchange, not one per
// goroutine. Run with -race to also confirm no data race on the shared
// token cache itself.
func TestConcurrentCallersShareOneTokenExchange(t *testing.T) {
	ctx := context.Background()
	const concurrentCallers = 50

	// A small delay widens the race window: without correct
	// serialization, every goroutine would observe an empty cache and
	// start its own token exchange before the first one completes.
	_, client, tokenCallCount := newTestServer(t, func(w http.ResponseWriter, r *http.Request, token string) {
		time.Sleep(5 * time.Millisecond)
		json.NewEncoder(w).Encode([]ChannelConfig{})
	})

	var wg sync.WaitGroup
	errs := make(chan error, concurrentCallers)
	for i := 0; i < concurrentCallers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.ListChannels(ctx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("ListChannels: %v", err)
	}
	if got := tokenCallCount(); got != 1 {
		t.Fatalf("got %d /auth/token exchanges for %d concurrent callers, want exactly 1", got, concurrentCallers)
	}
}
