// Shared configuration for all load test scripts.
// Override any value by setting the corresponding environment variable before
// invoking k6:
//
//   BASE_URL=https://staging.example.com k6 run loadtest/auth.js

export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// Admin credentials — used when a test needs cross-tenant access or tenant
// provisioning that only the admin role can perform.
export const ADMIN_API_KEY = __ENV.ADMIN_API_KEY || 'test-admin-key';
export const ADMIN_API_SECRET = __ENV.ADMIN_API_SECRET || 'test-admin-secret';

// Tenant credentials — pre-seeded by `make seed`; used for all tenant-scoped
// tests.
export const TENANT_API_KEY = __ENV.TENANT_API_KEY || 'test-api-key-123';
export const TENANT_API_SECRET = __ENV.TENANT_API_SECRET || 'test-secret-12345';
