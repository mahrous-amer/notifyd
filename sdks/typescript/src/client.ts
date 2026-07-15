import { NotifydConfigError, NotifydRequestError } from "./errors.js";
import { parseGoDuration } from "./duration.js";
import type {
  ApiErrorBody,
  ApiKey,
  ChannelConfig,
  CreateApiKeyResponse,
  CreateChannelInput,
  CreateWebhookInput,
  DeliveryAttempt,
  DeliveryMetric,
  ListNotificationsParams,
  Notification,
  NotificationList,
  SendInput,
  SendMultiResponse,
  UpdateChannelInput,
  UpdateWebhookInput,
  WebhookEndpoint,
  WebhookEndpointCreated,
} from "./types.js";

export const DEFAULT_BASE_URL = "https://notifyd.fluxintek.com/api";

export interface NotifydClientOptions {
  apiKey: string;
  apiSecret: string;
  /** Defaults to https://notifyd.fluxintek.com/api. */
  baseUrl?: string;
  /** Overrides the global fetch. Mainly for testing. */
  fetch?: typeof fetch;
}

interface TokenResponse {
  token: string;
  expires_in: string;
}

interface RequestOptions {
  method: string;
  path: string;
  query?: Record<string, string | number | undefined>;
  body?: unknown;
  expectEmpty?: boolean;
}

/** Official client for the notifyd notification delivery API. */
export class NotifydClient {
  private readonly apiKey: string;
  private readonly apiSecret: string;
  private readonly baseUrl: string;
  private readonly fetchImpl: typeof fetch;

  private cachedToken: string | undefined;
  private tokenExpiresAt = 0;
  /** Serializes concurrent token fetches onto one in-flight request. */
  private pendingTokenFetch: Promise<string> | undefined;

  constructor(options: NotifydClientOptions) {
    if (!options.apiKey || !options.apiSecret) {
      throw new NotifydConfigError("notifyd: apiKey and apiSecret are required");
    }
    this.apiKey = options.apiKey;
    this.apiSecret = options.apiSecret;
    this.baseUrl = options.baseUrl ?? DEFAULT_BASE_URL;
    this.fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);
  }

  // ---------------------------------------------------------------------
  // Token exchange
  // ---------------------------------------------------------------------

  /** Returns a cached JWT if it isn't near expiry, else exchanges for a fresh one. */
  private async authenticatedToken(): Promise<string> {
    const expiryMarginMs = 30_000;
    if (this.cachedToken && Date.now() + expiryMarginMs < this.tokenExpiresAt) {
      return this.cachedToken;
    }
    return this.fetchAndCacheToken();
  }

  private async fetchAndCacheToken(): Promise<string> {
    // Concurrent callers who both find the cache stale share one fetch
    // instead of each triggering their own /auth/token call.
    if (!this.pendingTokenFetch) {
      this.pendingTokenFetch = this.exchangeCredentialsForToken().finally(() => {
        this.pendingTokenFetch = undefined;
      });
    }
    return this.pendingTokenFetch;
  }

  private async exchangeCredentialsForToken(): Promise<string> {
    const response = await this.fetchImpl(`${this.baseUrl}/auth/token`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ api_key: this.apiKey, api_secret: this.apiSecret }),
    });

    const rawBody = await response.text();
    if (!response.ok) {
      throw buildRequestError(response.status, rawBody);
    }

    const parsed = JSON.parse(rawBody) as TokenResponse;
    this.cachedToken = parsed.token;
    this.tokenExpiresAt = Date.now() + parseGoDuration(parsed.expires_in);
    return this.cachedToken;
  }

  /** Discards the cached token and fetches a new one; used by the 401 retry path. */
  private async forceRefreshToken(): Promise<string> {
    this.cachedToken = undefined;
    return this.fetchAndCacheToken();
  }

  // ---------------------------------------------------------------------
  // Authenticated request path
  // ---------------------------------------------------------------------

  /**
   * Sends one authenticated API call, retrying exactly once with a forced
   * token refresh if the first attempt gets a 401. A second 401 after
   * refresh means the credentials themselves are bad, so it's surfaced as
   * a normal NotifydRequestError instead of retrying again.
   */
  private async request<T>(options: RequestOptions): Promise<T> {
    let token = await this.authenticatedToken();
    let response = await this.sendOnce(token, options);

    if (response.status === 401) {
      token = await this.forceRefreshToken();
      response = await this.sendOnce(token, options);
    }

    const rawBody = await response.text();
    if (!response.ok) {
      throw buildRequestError(response.status, rawBody);
    }
    if (options.expectEmpty || rawBody === "") {
      return undefined as T;
    }
    return JSON.parse(rawBody) as T;
  }

  private async sendOnce(token: string, options: RequestOptions): Promise<Response> {
    const url = new URL(`${this.baseUrl}${options.path}`);
    for (const [key, value] of Object.entries(options.query ?? {})) {
      if (value !== undefined) {
        url.searchParams.set(key, String(value));
      }
    }

    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    if (options.body !== undefined) {
      headers["Content-Type"] = "application/json";
    }

    return this.fetchImpl(url.toString(), {
      method: options.method,
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    });
  }

  // ---------------------------------------------------------------------
  // Notifications
  // ---------------------------------------------------------------------

  /**
   * Enqueues a single notification for async delivery. Returns immediately
   * with the notification in "pending" status; poll getNotification for
   * the terminal outcome.
   */
  async send(input: SendInput): Promise<Notification> {
    return this.request<Notification>({
      method: "POST",
      path: "/notifications/send",
      body: {
        channel_config_id: input.channelConfigId,
        subject: input.subject,
        body: input.body,
        metadata: input.metadata,
      },
    });
  }

  /**
   * Enqueues notifications across up to 50 channel configs in one request.
   * Partial success is expected: check the returned `errors` array even
   * when the call itself doesn't throw.
   */
  async sendMulti(inputs: SendInput[]): Promise<SendMultiResponse> {
    return this.request<SendMultiResponse>({
      method: "POST",
      path: "/notifications/send-multi",
      body: {
        channels: inputs.map((input) => ({
          channel_config_id: input.channelConfigId,
          subject: input.subject,
          body: input.body,
          metadata: input.metadata,
        })),
      },
    });
  }

  /** Returns a page of the authenticated tenant's notifications. */
  async listNotifications(params: ListNotificationsParams = {}): Promise<NotificationList> {
    return this.request<NotificationList>({
      method: "GET",
      path: "/notifications",
      query: {
        limit: params.limit,
        offset: params.offset,
        status: params.status,
        channel: params.channel,
      },
    });
  }

  /** Fetches one notification by ID. */
  async getNotification(notificationId: string): Promise<Notification> {
    return this.request<Notification>({
      method: "GET",
      path: `/notifications/${notificationId}`,
    });
  }

  /** Returns every delivery attempt recorded for a notification, ordered by attempt number. */
  async listAttempts(notificationId: string): Promise<DeliveryAttempt[]> {
    return this.request<DeliveryAttempt[]>({
      method: "GET",
      path: `/notifications/${notificationId}/attempts`,
    });
  }

  /**
   * Returns provider-reported engagement metrics for a delivered
   * notification. Throws NotifydRequestError with statusCode 404 if
   * metrics haven't been collected yet.
   */
  async getMetrics(notificationId: string): Promise<DeliveryMetric> {
    return this.request<DeliveryMetric>({
      method: "GET",
      path: `/notifications/${notificationId}/metrics`,
    });
  }

  // ---------------------------------------------------------------------
  // Channels
  // ---------------------------------------------------------------------

  /** Returns all channel configs belonging to the authenticated tenant. */
  async listChannels(): Promise<ChannelConfig[]> {
    return this.request<ChannelConfig[]>({ method: "GET", path: "/channels" });
  }

  /** Creates a new channel config for the authenticated tenant. */
  async createChannel(input: CreateChannelInput): Promise<ChannelConfig> {
    return this.request<ChannelConfig>({ method: "POST", path: "/channels", body: input });
  }

  /** Fetches one channel config by ID. */
  async getChannel(channelId: string): Promise<ChannelConfig> {
    return this.request<ChannelConfig>({ method: "GET", path: `/channels/${channelId}` });
  }

  /** Updates a channel config. Fields omitted from `input` are left unchanged. */
  async updateChannel(channelId: string, input: UpdateChannelInput): Promise<ChannelConfig> {
    return this.request<ChannelConfig>({
      method: "PATCH",
      path: `/channels/${channelId}`,
      body: input,
    });
  }

  /** Deletes a channel config by ID. */
  async deleteChannel(channelId: string): Promise<void> {
    await this.request<void>({
      method: "DELETE",
      path: `/channels/${channelId}`,
      expectEmpty: true,
    });
  }

  // ---------------------------------------------------------------------
  // API keys
  // ---------------------------------------------------------------------

  /** Returns all API keys for the authenticated tenant. Secret hashes are never included. */
  async listApiKeys(): Promise<ApiKey[]> {
    return this.request<ApiKey[]>({ method: "GET", path: "/keys" });
  }

  /**
   * Creates a new API key. The returned api_secret is shown only in this
   * response — save it immediately.
   */
  async createApiKey(label: string): Promise<CreateApiKeyResponse> {
    return this.request<CreateApiKeyResponse>({
      method: "POST",
      path: "/keys",
      body: { label },
    });
  }

  /** Revokes an API key by ID. */
  async revokeApiKey(keyId: string): Promise<void> {
    await this.request<void>({ method: "DELETE", path: `/keys/${keyId}`, expectEmpty: true });
  }

  // ---------------------------------------------------------------------
  // Webhooks
  // ---------------------------------------------------------------------

  /** Returns all status-webhook endpoints belonging to the authenticated tenant. */
  async listWebhooks(): Promise<WebhookEndpoint[]> {
    return this.request<WebhookEndpoint[]>({ method: "GET", path: "/webhooks" });
  }

  /**
   * Registers a new destination for notification.delivered /
   * notification.failed status events. The returned secret is shown only
   * in this response — save it, then verify deliveries with
   * verifyWebhookSignature.
   */
  async createWebhook(input: CreateWebhookInput): Promise<WebhookEndpointCreated> {
    return this.request<WebhookEndpointCreated>({
      method: "POST",
      path: "/webhooks",
      body: input,
    });
  }

  /**
   * Updates a webhook endpoint. Fields omitted from `input` are left
   * unchanged. Never returns the secret.
   */
  async updateWebhook(webhookId: string, input: UpdateWebhookInput): Promise<WebhookEndpoint> {
    return this.request<WebhookEndpoint>({
      method: "PUT",
      path: `/webhooks/${webhookId}`,
      body: input,
    });
  }

  /** Deletes a webhook endpoint by ID. */
  async deleteWebhook(webhookId: string): Promise<void> {
    await this.request<void>({
      method: "DELETE",
      path: `/webhooks/${webhookId}`,
      expectEmpty: true,
    });
  }
}

function buildRequestError(statusCode: number, rawBody: string): NotifydRequestError {
  try {
    const parsed = JSON.parse(rawBody) as ApiErrorBody;
    if (parsed && typeof parsed.error === "string") {
      return new NotifydRequestError(statusCode, parsed, rawBody);
    }
  } catch {
    // Body wasn't JSON (e.g. an upstream proxy's HTML error page) — fall
    // through to the raw-body error below.
  }
  return new NotifydRequestError(statusCode, undefined, rawBody);
}
