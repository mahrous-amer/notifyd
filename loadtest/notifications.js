/**
 * Load test: POST /notifications/send  +  GET /notifications/{id}
 *
 * Each VU sends a single notification through a Discord webhook channel config
 * created during setup, then immediately fetches the resulting notification
 * record to exercise both the write and read paths together.
 *
 * The Discord webhook URL is intentionally a mock — the worker will attempt
 * delivery and fail gracefully; we are only measuring the API layer here.
 *
 * Run:
 *   k6 run loadtest/notifications.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import {
  BASE_URL,
  TENANT_API_KEY,
  TENANT_API_SECRET,
} from './config.js';

const errorRate   = new Rate('errors');
const sendLatency = new Trend('send_latency',  true);
const fetchLatency = new Trend('fetch_latency', true);

export const options = {
  stages: [
    { duration: '30s', target: 100 },
    { duration: '2m',  target: 100 },
    { duration: '30s', target: 0   },
  ],
  thresholds: {
    http_req_duration: ['p(95)<1000', 'p(99)<2000'],
    errors:            ['rate<0.05'],
  },
};

// ─── Setup ───────────────────────────────────────────────────────────────────

export function setup() {
  const token         = authenticateTenant(TENANT_API_KEY, TENANT_API_SECRET);
  const channelConfig = createDiscordChannelConfig(token);

  return { token, channelConfigId: channelConfig.id };
}

// ─── Main VU function ────────────────────────────────────────────────────────

export default function (data) {
  const authHeaders = buildAuthHeaders(data.token);

  const sendResponse = sendNotification(data.channelConfigId, authHeaders);
  sendLatency.add(sendResponse.timings.duration);

  const sendOk = check(sendResponse, {
    'send: status 202': (r) => r.status === 202,
    'send: id present': (r) => r.json('id') !== '',
  });

  errorRate.add(!sendOk);

  if (!sendOk) {
    return;
  }

  const notificationId  = sendResponse.json('id');
  const fetchResponse   = fetchNotification(notificationId, authHeaders);
  fetchLatency.add(fetchResponse.timings.duration);

  const fetchOk = check(fetchResponse, {
    'fetch: status 200': (r) => r.status === 200,
    'fetch: id matches': (r) => r.json('id') === notificationId,
  });

  errorRate.add(!fetchOk);

  sleep(0.5);
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

function createDiscordChannelConfig(token) {
  const payload = JSON.stringify({
    channel: 'discord',
    name:    'load-test-discord',
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

function sendNotification(channelConfigId, headers) {
  const payload = JSON.stringify({
    channel_config_id: channelConfigId,
    subject:           'Load test notification',
    body:              'This notification was sent by the k6 load test suite.',
    metadata:          { source: 'k6', test: 'notifications' },
  });

  return http.post(`${BASE_URL}/notifications/send`, payload, { headers });
}

function fetchNotification(notificationId, headers) {
  return http.get(`${BASE_URL}/notifications/${notificationId}`, { headers });
}

function buildAuthHeaders(token) {
  return {
    'Content-Type':  'application/json',
    'Authorization': `Bearer ${token}`,
  };
}
