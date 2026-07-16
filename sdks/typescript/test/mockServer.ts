import { NotifydClient } from "../src/client.js";

export interface MockRequest {
  method: string;
  path: string;
  searchParams: URLSearchParams;
  bearerToken: string;
  body: unknown;
}

export type MockHandler = (request: MockRequest) => {
  status: number;
  body?: unknown;
};

interface MockServerResult {
  client: NotifydClient;
  /** All non-token requests the handler observed, in order. */
  requests: MockRequest[];
  /** Bearer tokens presented on each /auth/token exchange's *response*. */
  issuedTokens: string[];
}

/**
 * Builds a NotifydClient wired to a fake fetch instead of a real HTTP
 * server. Mirrors the Go SDK's httptest-based newTestServer: issues a
 * fresh "test-token-<n>" on every /auth/token call so tests can tell a
 * cached token from a freshly-issued one, and routes every other request
 * through `handler`.
 */
export function newMockServer(handler: MockHandler): MockServerResult {
  const requests: MockRequest[] = [];
  const issuedTokens: string[] = [];
  let tokenCallCount = 0;

  const fakeFetch: typeof fetch = async (input, init) => {
    const url = new URL(typeof input === "string" ? input : input.toString());
    const method = init?.method ?? "GET";
    const parsedBody = init?.body ? JSON.parse(init.body as string) : undefined;

    if (url.pathname === "/auth/token") {
      tokenCallCount += 1;
      if (parsedBody.api_key !== "test-key" || parsedBody.api_secret !== "test-secret") {
        return jsonResponse(401, { error: "INVALID_CREDENTIALS" });
      }
      const token = `test-token-${tokenCallCount}`;
      issuedTokens.push(token);
      return jsonResponse(200, { token, expires_in: "24h0m0s" });
    }

    const authHeader = (init?.headers as Record<string, string> | undefined)?.Authorization ?? "";
    const bearerToken = authHeader.replace(/^Bearer /, "");

    const request: MockRequest = {
      method,
      path: url.pathname,
      searchParams: url.searchParams,
      bearerToken,
      body: parsedBody,
    };
    requests.push(request);

    const { status, body } = handler(request);
    return jsonResponse(status, body);
  };

  const client = new NotifydClient({
    apiKey: "test-key",
    apiSecret: "test-secret",
    baseUrl: "https://mock.invalid",
    fetch: fakeFetch,
  });

  return { client, requests, issuedTokens };
}

function jsonResponse(status: number, body?: unknown): Response {
  // The Fetch spec forbids a body on null-body statuses (204/205/304); the
  // Response constructor throws if one is passed.
  const isNullBodyStatus = status === 204 || status === 205 || status === 304;
  const responseBody = body === undefined || isNullBodyStatus ? null : JSON.stringify(body);
  return new Response(responseBody, {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
