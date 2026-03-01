/**
 * Load test: read-only endpoints
 *
 * Exercises the four read paths under high concurrency:
 *   GET /notifications      — paginated list
 *   GET /notifications/{id} — single notification lookup
 *   GET /channels           — channel config list
 *   GET /health             — health probe (unauthenticated)
 *
 * The mix is weighted toward listing notifications (the most common read
 * pattern in production) while still exercising every endpoint each iteration.
 *
 * Run:
 *   k6 run loadtest/queries.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import {
  BASE_URL,
  TENANT_API_KEY,
  TENANT_API_SECRET,
} from './config.js';

const errorRate = new Rate('errors');

export const options = {
  stages: [
    { duration: '30s', target: 200 },
    { duration: '1m',  target: 200 },
    { duration: '15s', target: 0   },
  ],
  thresholds: {
    http_req_duration: ['p(95)<300'],
    errors:            ['rate<0.01'],
  },
};

// ─── Setup ───────────────────────────────────────────────────────────────────

export function setup() {
  const token          = authenticateTenant(TENANT_API_KEY, TENANT_API_SECRET);
  const notificationId = seedOneNotification(token);

  return { token, notificationId };
}

// ─── Main VU function ────────────────────────────────────────────────────────

export default function (data) {
  const authHeaders = buildAuthHeaders(data.token);

  const listNotifOk   = checkListNotifications(authHeaders);
  const getNotifOk    = checkGetNotification(data.notificationId, authHeaders);
  const listChannelOk = checkListChannels(authHeaders);
  const healthOk      = checkHealth();

  errorRate.add(!listNotifOk || !getNotifOk || !listChannelOk || !healthOk);

  sleep(0.1);
}

// ─── Per-endpoint checks ──────────────────────────────────────────────────────

function checkListNotifications(headers) {
  const response = http.get(`${BASE_URL}/notifications?limit=20&offset=0`, { headers });
  return check(response, {
    'list notifications: status 200': (r) => r.status === 200,
    'list notifications: data array': (r) => Array.isArray(r.json('data')),
  });
}

function checkGetNotification(notificationId, headers) {
  const response = http.get(`${BASE_URL}/notifications/${notificationId}`, { headers });
  return check(response, {
    'get notification: status 200':  (r) => r.status === 200,
    'get notification: id matches':  (r) => r.json('id') === notificationId,
  });
}

function checkListChannels(headers) {
  const response = http.get(`${BASE_URL}/channels`, { headers });
  return check(response, {
    'list channels: status 200': (r) => r.status === 200,
  });
}

function checkHealth() {
  const response = http.get(`${BASE_URL}/health`);
  return check(response, {
    'health: status 200': (r) => r.status === 200,
  });
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function authenticateTenant(apiKey, apiSecret) {
  const payload  = JSON.stringify({ api_key: apiKey, api_secret: apiSecret });
  const params   = { headers: { 'Content-Type': 'application/json' } };
  const response = http.post(`${BASE_URL}/auth/token`, payload, params);

  if (response.status !== 200) {
    throw new Error(`setup: authentication failed with status ${response.status}`);
  }

  return response.json('token');
}

// Creates a channel config and sends one notification so the VU loop has a
// real notification ID to query from the very first iteration.
function seedOneNotification(token) {
  const channelConfig = createDiscordChannelConfig(token);
  return sendOneNotification(token, channelConfig.id);
}

function createDiscordChannelConfig(token) {
  const payload = JSON.stringify({
    channel: 'discord',
    name:    'load-test-queries',
    config:  { webhook_url: 'https://discord.com/api/webhooks/000000000000000000/mock-load-test-token' },
  });

  const response = http.post(`${BASE_URL}/channels`, payload, {
    headers: {
      'Content-Type':  'application/json',
      'Authorization': `Bearer ${token}`,
    },
  });

  if (response.status !== 201) {
    throw new Error(`setup: channel config creation failed with status ${response.status}: ${response.body}`);
  }

  return response.json();
}

function sendOneNotification(token, channelConfigId) {
  const payload = JSON.stringify({
    channel_config_id: channelConfigId,
    body:              'Seed notification for queries load test.',
  });

  const response = http.post(`${BASE_URL}/notifications/send`, payload, {
    headers: {
      'Content-Type':  'application/json',
      'Authorization': `Bearer ${token}`,
    },
  });

  if (response.status !== 202) {
    throw new Error(`setup: seed notification failed with status ${response.status}: ${response.body}`);
  }

  return response.json('id');
}

function buildAuthHeaders(token) {
  return {
    'Content-Type':  'application/json',
    'Authorization': `Bearer ${token}`,
  };
}
