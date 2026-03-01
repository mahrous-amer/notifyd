/**
 * Load test: full combined scenario
 *
 * Runs four named scenarios in parallel, each modelling a distinct traffic
 * pattern observed in production:
 *
 *   auth_flow            — constant 10 VUs cycling through token issuance.
 *                          Detects auth latency regressions under steady load.
 *
 *   send_notifications   — ramping write load up to 50 VUs, each sending one
 *                          notification per iteration.
 *
 *   query_notifications  — ramping read load up to 100 VUs exercising the list
 *                          and get-by-ID endpoints.
 *
 *   health_checks        — constant 5 VUs polling the health endpoint at a
 *                          high rate to confirm it never saturates.
 *
 * All scenarios share a single setup phase that produces the credentials and
 * seed data consumed by the VU functions.
 *
 * Per-scenario thresholds use tag filtering so that the p95 budget for health
 * checks (50 ms) does not contaminate the budget for write operations (1 s).
 *
 * Run:
 *   k6 run loadtest/full_scenario.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import {
  BASE_URL,
  TENANT_API_KEY,
  TENANT_API_SECRET,
} from './config.js';

// ─── Custom metrics ───────────────────────────────────────────────────────────

const authErrors   = new Rate('auth_errors');
const sendErrors   = new Rate('send_errors');
const queryErrors  = new Rate('query_errors');
const healthErrors = new Rate('health_errors');

const authDuration   = new Trend('auth_duration',   true);
const sendDuration   = new Trend('send_duration',   true);
const queryDuration  = new Trend('query_duration',  true);
const healthDuration = new Trend('health_duration', true);

// ─── Scenario configuration ───────────────────────────────────────────────────

export const options = {
  scenarios: {
    auth_flow: {
      executor:   'constant-vus',
      vus:        10,
      duration:   '2m',
      exec:       'runAuthFlow',
      tags:       { scenario: 'auth_flow' },
      startTime:  '0s',
    },

    send_notifications: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 50 },
        { duration: '1m',  target: 50 },
        { duration: '30s', target: 0  },
      ],
      exec:      'runSendNotification',
      tags:      { scenario: 'send_notifications' },
      startTime: '0s',
    },

    query_notifications: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 100 },
        { duration: '1m',  target: 100 },
        { duration: '30s', target: 0   },
      ],
      exec:      'runQueryNotifications',
      tags:      { scenario: 'query_notifications' },
      startTime: '0s',
    },

    health_checks: {
      executor:  'constant-vus',
      vus:       5,
      duration:  '2m',
      exec:      'runHealthCheck',
      tags:      { scenario: 'health_checks' },
      startTime: '0s',
    },
  },

  thresholds: {
    // Broad HTTP budget covering all scenarios.
    http_req_duration: ['p(95)<1000', 'p(99)<2000'],

    // Per-scenario custom-metric budgets.
    auth_duration:   ['p(95)<500'],
    send_duration:   ['p(95)<1000', 'p(99)<2000'],
    query_duration:  ['p(95)<300'],
    health_duration: ['p(95)<50'],

    // Error-rate budgets per scenario.
    auth_errors:   ['rate<0.01'],
    send_errors:   ['rate<0.05'],
    query_errors:  ['rate<0.01'],
    health_errors: ['rate<0.001'],
  },
};

// ─── Setup ───────────────────────────────────────────────────────────────────

export function setup() {
  const token          = authenticateTenant(TENANT_API_KEY, TENANT_API_SECRET);
  const channelConfig  = createDiscordChannelConfig(token);
  const notificationId = seedOneNotification(token, channelConfig.id);

  return { token, channelConfigId: channelConfig.id, notificationId };
}

// ─── Scenario: auth_flow ─────────────────────────────────────────────────────

export function runAuthFlow() {
  const response = requestToken(TENANT_API_KEY, TENANT_API_SECRET);
  authDuration.add(response.timings.duration);

  const succeeded = check(response, {
    'auth: status 200':       (r) => r.status === 200,
    'auth: token not empty':  (r) => r.json('token') !== '',
  });

  authErrors.add(!succeeded);
  sleep(0.5);
}

// ─── Scenario: send_notifications ────────────────────────────────────────────

export function runSendNotification(data) {
  const payload  = buildSendPayload(data.channelConfigId);
  const response = http.post(`${BASE_URL}/notifications/send`, payload, {
    headers: buildAuthHeaders(data.token),
    tags:    { operation: 'send' },
  });

  sendDuration.add(response.timings.duration);

  const succeeded = check(response, {
    'send: status 202': (r) => r.status === 202,
    'send: id present': (r) => r.json('id') !== '',
  });

  sendErrors.add(!succeeded);
  sleep(0.3);
}

// ─── Scenario: query_notifications ───────────────────────────────────────────

export function runQueryNotifications(data) {
  const headers = buildAuthHeaders(data.token);

  const listResponse = http.get(`${BASE_URL}/notifications?limit=20&offset=0`, {
    headers,
    tags: { operation: 'list' },
  });
  queryDuration.add(listResponse.timings.duration);

  const listOk = check(listResponse, {
    'list: status 200':  (r) => r.status === 200,
    'list: data exists': (r) => Array.isArray(r.json('data')),
  });

  const getResponse = http.get(`${BASE_URL}/notifications/${data.notificationId}`, {
    headers,
    tags: { operation: 'get' },
  });
  queryDuration.add(getResponse.timings.duration);

  const getOk = check(getResponse, {
    'get: status 200': (r) => r.status === 200,
    'get: id matches': (r) => r.json('id') === data.notificationId,
  });

  queryErrors.add(!listOk || !getOk);
  sleep(0.1);
}

// ─── Scenario: health_checks ─────────────────────────────────────────────────

export function runHealthCheck() {
  const response = http.get(`${BASE_URL}/health`, {
    tags: { operation: 'health' },
  });

  healthDuration.add(response.timings.duration);

  const succeeded = check(response, {
    'health: status 200': (r) => r.status === 200,
  });

  healthErrors.add(!succeeded);
  sleep(0.2);
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
    name:    'load-test-full-scenario',
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

function seedOneNotification(token, channelConfigId) {
  const payload = JSON.stringify({
    channel_config_id: channelConfigId,
    body:              'Seed notification for full-scenario load test.',
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

function requestToken(apiKey, apiSecret) {
  const payload = JSON.stringify({ api_key: apiKey, api_secret: apiSecret });
  return http.post(`${BASE_URL}/auth/token`, payload, {
    headers: { 'Content-Type': 'application/json' },
  });
}

function buildSendPayload(channelConfigId) {
  return JSON.stringify({
    channel_config_id: channelConfigId,
    subject:           'Full-scenario load test notification',
    body:              'Sent by the k6 full_scenario load test.',
    metadata:          { source: 'k6', test: 'full_scenario' },
  });
}

function buildAuthHeaders(token) {
  return {
    'Content-Type':  'application/json',
    'Authorization': `Bearer ${token}`,
  };
}
