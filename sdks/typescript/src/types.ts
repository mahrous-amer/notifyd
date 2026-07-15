/** One of the delivery channels notifyd supports. */
export type ChannelType =
  | "discord"
  | "telegram"
  | "whatsapp"
  | "email"
  | "slack"
  | "webhook";

/** The delivery lifecycle state of a Notification. */
export type NotificationStatus =
  | "pending"
  | "processing"
  | "delivered"
  | "failed"
  | "retrying";

/** The outcome of a single delivery attempt. */
export type AttemptStatus = "success" | "failure";

/** Controls message rendering mode for channels that support rich formatting. */
export type FormatMode = "plain" | "markdown" | "html";

/** The shape of every non-2xx JSON response body. */
export interface ApiErrorBody {
  error: string;
  [extra: string]: unknown;
}

export interface DeliveryPreferences {
  priority?: "critical" | "normal" | "low";
  max_retries?: number;
  format_mode?: FormatMode;
}

/** A tenant's configured destination for one channel type. */
export interface ChannelConfig {
  id: string;
  tenant_id: string;
  channel: ChannelType;
  name: string;
  /** Provider-specific configuration; shape depends on `channel`. */
  config: Record<string, unknown>;
  is_active: boolean;
  delivery_prefs?: DeliveryPreferences;
  created_at: string;
  updated_at: string;
}

export interface CreateChannelInput {
  channel: ChannelType;
  name: string;
  config: Record<string, unknown>;
  delivery_prefs?: DeliveryPreferences;
}

/** Fields omitted here are left unchanged server-side. */
export interface UpdateChannelInput {
  name?: string;
  config?: Record<string, unknown>;
  is_active?: boolean;
  delivery_prefs?: DeliveryPreferences;
}

export interface Notification {
  id: string;
  tenant_id: string;
  channel_config_id: string;
  channel: ChannelType;
  subject: string | null;
  body: string;
  metadata?: Record<string, unknown>;
  status: NotificationStatus;
  retry_count: number;
  max_retries: number;
  last_error: string | null;
  delivered_at: string | null;
  provider_msg_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface SendInput {
  channelConfigId: string;
  /** Optional for chat/webhook channels; required and non-blank for email. */
  subject?: string;
  body: string;
  metadata?: Record<string, unknown>;
}

export interface SendMultiResponse {
  sent: Notification[];
  /** Per-item error messages for any channels that failed to enqueue. */
  errors: string[];
}

export interface NotificationList {
  data: Notification[];
  total: number;
  limit: number;
  offset: number;
}

export interface ListNotificationsParams {
  limit?: number;
  offset?: number;
  status?: NotificationStatus;
  channel?: ChannelType;
}

export interface DeliveryAttempt {
  id: string;
  notification_id: string;
  attempt_number: number;
  status: AttemptStatus;
  provider_response?: Record<string, unknown>;
  error_message: string | null;
  duration_ms: number;
  attempted_at: string;
}

export interface DeliveryMetric {
  id: string;
  notification_id: string;
  provider_msg_id: string;
  delivered_at: string | null;
  read_at: string | null;
  interactions?: Record<string, unknown>;
  collected_at: string;
}

/** Never includes the secret — see CreateApiKeyResponse for the one response that does. */
export interface ApiKey {
  id: string;
  tenant_id: string;
  api_key: string;
  label: string;
  created_at: string;
  revoked_at: string | null;
}

export interface CreateApiKeyResponse {
  key: ApiKey;
  /** Plain-text secret shown only at creation time. */
  api_secret: string;
}

/** Status events a webhook endpoint may subscribe to. */
export type WebhookEventType = "notification.delivered" | "notification.failed";

/** The signing secret is never included here — see WebhookEndpointCreated. */
export interface WebhookEndpoint {
  id: string;
  tenant_id: string;
  url: string;
  events: WebhookEventType[];
  is_active: boolean;
  created_at: string;
}

export interface WebhookEndpointCreated extends WebhookEndpoint {
  /** Plaintext signing secret, shown once. */
  secret: string;
}

export interface CreateWebhookInput {
  url: string;
  events: WebhookEventType[];
}

/** Fields omitted here are left unchanged server-side. */
export interface UpdateWebhookInput {
  url?: string;
  events?: WebhookEventType[];
  is_active?: boolean;
}
