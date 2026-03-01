/**
 * Load test: POST /notifications/send-multi
 *
 * Each VU sends a batch of 5 notifications in a single request across the 3
 * Discord channel configs created during setup. This tests the batch-write
 * code path and measures how latency scales with payload size.
 *
 * Run:
 *   k6 run loadtest/send_multi.js
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
    { duration: '30s', target: 50 },
    { duration: '1m',  target: 50 },
    { duration: '15s', target: 0  },
  ],
  thresholds: {
    http_req_duration: ['p(95)<2000'],
    errors:            ['rate<0.05'],
  },
};

// How many notifications to include per send-multi call.
const NOTIFICATIONS_PER_BATCH = 5;

// ─── Setup ───────────────────────────────────────────────────────────────────

export function setup() {
  const token          = authenticateTenant(TENANT_API_KEY, TENANT_API_SECRET);
  const channelConfigs = createThreeDiscordChannelConfigs(token);

  return { token, channelConfigIds: channelConfigs.map((c) => c.id) };
}

// ─── Main VU function ────────────────────────────────────────────────────────

export default function (data) {
  const channels = buildBatchChannels(data.channelConfigIds, NOTIFICATIONS_PER_BATCH);
  const response = sendMultiNotification(channels, data.token);

  const succeeded = check(response, {
    'status is 202':     (r) => r.status === 202,
    'sent array exists': (r) => Array.isArray(r.json('sent')),
  });

  errorRate.add(!succeeded);

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

function createDiscordChannelConfig(token, name) {
  const payload = JSON.stringify({
    channel: 'discord',
    name,
    config: { webhook_url: 'https://discord.com/api/webhooks/000000000000000000/mock-load-test-token' },
  });

  const response = http.post(`${BASE_URL}/channels`, payload, {
    headers: {
      'Content-Type':  'application/json',
      'Authorization': `Bearer ${token}`,
    },
  });

  if (response.status !== 201) {
    throw new Error(`setup: channel config "${name}" creation failed with status ${response.status}: ${response.body}`);
  }

  return response.json();
}

function createThreeDiscordChannelConfigs(token) {
  return [
    createDiscordChannelConfig(token, 'load-test-multi-1'),
    createDiscordChannelConfig(token, 'load-test-multi-2'),
    createDiscordChannelConfig(token, 'load-test-multi-3'),
  ];
}

// Builds an array of SendNotificationInput objects, cycling through the
// available channel config IDs so the batch is spread across all three.
function buildBatchChannels(channelConfigIds, count) {
  const channels = [];

  for (let i = 0; i < count; i++) {
    channels.push({
      channel_config_id: channelConfigIds[i % channelConfigIds.length],
      subject:           `Batch notification ${i + 1}`,
      body:              `Batch item ${i + 1} sent by the k6 send-multi load test.`,
      metadata:          { source: 'k6', test: 'send_multi', index: i },
    });
  }

  return channels;
}

function sendMultiNotification(channels, token) {
  const payload = JSON.stringify({ channels });
  return http.post(`${BASE_URL}/notifications/send-multi`, payload, {
    headers: {
      'Content-Type':  'application/json',
      'Authorization': `Bearer ${token}`,
    },
  });
}
