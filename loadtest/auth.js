/**
 * Load test: POST /auth/token
 *
 * Measures the raw throughput and latency of the token-issuance endpoint
 * under a realistic ramp-up pattern. Each VU authenticates once per iteration
 * using the pre-seeded tenant credentials.
 *
 * Run:
 *   k6 run loadtest/auth.js
 *   BASE_URL=http://localhost:8080 k6 run loadtest/auth.js
 */

import http from 'k6/http';
import { check } from 'k6';
import { Rate } from 'k6/metrics';
import { BASE_URL, TENANT_API_KEY, TENANT_API_SECRET } from './config.js';

const errorRate = new Rate('errors');

export const options = {
  stages: [
    { duration: '30s', target: 50 },
    { duration: '1m',  target: 50 },
    { duration: '15s', target: 0  },
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],
    errors:            ['rate<0.01'],
  },
};

export default function () {
  const response = requestToken(TENANT_API_KEY, TENANT_API_SECRET);

  const succeeded = check(response, {
    'status is 200':        (r) => r.status === 200,
    'token field present':  (r) => r.json('token') !== '',
    'expires_in present':   (r) => r.json('expires_in') !== '',
  });

  errorRate.add(!succeeded);
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function requestToken(apiKey, apiSecret) {
  const payload = JSON.stringify({ api_key: apiKey, api_secret: apiSecret });
  const params  = { headers: { 'Content-Type': 'application/json' } };

  return http.post(`${BASE_URL}/auth/token`, payload, params);
}
