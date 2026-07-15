import { describe, expect, it } from "vitest";
import { NotifydClient } from "../src/client.js";
import { NotifydConfigError, NotifydRequestError } from "../src/errors.js";
import { newMockServer } from "./mockServer.js";

describe("token exchange and caching", () => {
  it("reuses the cached token across multiple calls", async () => {
    let channelsCalls = 0;
    const { client } = newMockServer((request) => {
      expect(request.path).toBe("/channels");
      channelsCalls += 1;
      expect(request.bearerToken).toBe("test-token-1");
      return { status: 200, body: [] };
    });

    await client.listChannels();
    await client.listChannels();

    expect(channelsCalls).toBe(2);
  });

  it("throws NotifydConfigError when credentials are missing", () => {
    expect(() => new NotifydClient({ apiKey: "", apiSecret: "" })).toThrow(NotifydConfigError);
    expect(() => new NotifydClient({ apiKey: "k", apiSecret: "" })).toThrow(NotifydConfigError);
  });
});

describe("refresh-once-on-401", () => {
  it("retries exactly once with a fresh token after a 401", async () => {
    const seenTokens: string[] = [];
    const { client } = newMockServer((request) => {
      seenTokens.push(request.bearerToken);
      if (request.bearerToken === "test-token-1") {
        return { status: 401, body: { error: "TOKEN_EXPIRED" } };
      }
      return { status: 200, body: [] };
    });

    await client.listChannels();

    expect(seenTokens).toEqual(["test-token-1", "test-token-2"]);
  });

  it("gives up after one retry instead of looping on persistent 401s", async () => {
    let attempts = 0;
    const { client } = newMockServer(() => {
      attempts += 1;
      return { status: 401, body: { error: "INVALID_TOKEN" } };
    });

    await expect(client.listChannels()).rejects.toThrow(NotifydRequestError);
    expect(attempts).toBe(2);
  });

  it("surfaces the error code from the final 401", async () => {
    const { client } = newMockServer(() => ({ status: 401, body: { error: "INVALID_TOKEN" } }));

    try {
      await client.listChannels();
      expect.unreachable("expected listChannels to throw");
    } catch (error) {
      expect(error).toBeInstanceOf(NotifydRequestError);
      expect((error as NotifydRequestError).statusCode).toBe(401);
      expect((error as NotifydRequestError).code).toBe("INVALID_TOKEN");
    }
  });
});

describe("notifications", () => {
  it("sends a notification", async () => {
    const { client, requests } = newMockServer((request) => {
      if (request.path === "/notifications/send") {
        return { status: 202, body: { id: "notif-1", status: "pending" } };
      }
      throw new Error(`unexpected request: ${request.method} ${request.path}`);
    });

    const notification = await client.send({ channelConfigId: "chan-1", body: "hello" });

    expect(notification.id).toBe("notif-1");
    expect(notification.status).toBe("pending");
    expect(requests[0].body).toEqual({
      channel_config_id: "chan-1",
      subject: undefined,
      body: "hello",
      metadata: undefined,
    });
  });

  it("exposes upgrade_url from a 429 QUOTA_EXCEEDED body", async () => {
    const { client } = newMockServer((request) => {
      expect(request.path).toBe("/notifications/send");
      return {
        status: 429,
        body: {
          error: "QUOTA_EXCEEDED",
          upgrade_url: "https://notifyd.fluxintek.com/account/upgrade",
        },
      };
    });

    try {
      await client.send({ channelConfigId: "chan-1", body: "hello" });
      expect.unreachable("expected send to throw");
    } catch (error) {
      expect(error).toBeInstanceOf(NotifydRequestError);
      const requestError = error as NotifydRequestError;
      expect(requestError.statusCode).toBe(429);
      expect(requestError.code).toBe("QUOTA_EXCEEDED");
      expect(requestError.body?.upgrade_url).toBe("https://notifyd.fluxintek.com/account/upgrade");
      expect(requestError.field("upgrade_url")).toBe("https://notifyd.fluxintek.com/account/upgrade");
    }
  });

  it("sends to multiple channels and surfaces partial errors", async () => {
    const { client } = newMockServer((request) => {
      expect(request.path).toBe("/notifications/send-multi");
      return {
        status: 202,
        body: {
          sent: [{ id: "notif-1" }],
          errors: ["chan-2: CHANNEL_NOT_FOUND"],
        },
      };
    });

    const result = await client.sendMulti([
      { channelConfigId: "chan-1", body: "a" },
      { channelConfigId: "chan-2", body: "b" },
    ]);

    expect(result.sent).toHaveLength(1);
    expect(result.errors).toEqual(["chan-2: CHANNEL_NOT_FOUND"]);
  });

  it("lists notifications with query filters", async () => {
    const { client, requests } = newMockServer((request) => {
      expect(request.path).toBe("/notifications");
      expect(request.searchParams.get("status")).toBe("delivered");
      return {
        status: 200,
        body: { data: [{ id: "notif-1", status: "delivered" }], total: 1, limit: 20, offset: 0 },
      };
    });

    const list = await client.listNotifications({ status: "delivered" });

    expect(list.total).toBe(1);
    expect(requests[0].searchParams.get("status")).toBe("delivered");
  });

  it("gets a notification by id", async () => {
    const { client } = newMockServer((request) => {
      expect(request.path).toBe("/notifications/notif-1");
      return { status: 200, body: { id: "notif-1", status: "delivered" } };
    });

    const notification = await client.getNotification("notif-1");

    expect(notification.status).toBe("delivered");
  });

  it("lists delivery attempts", async () => {
    const { client } = newMockServer((request) => {
      expect(request.path).toBe("/notifications/notif-1/attempts");
      return { status: 200, body: [{ id: "attempt-1", attempt_number: 1, status: "success" }] };
    });

    const attempts = await client.listAttempts("notif-1");

    expect(attempts).toHaveLength(1);
    expect(attempts[0].status).toBe("success");
  });
});

describe("channels", () => {
  it("supports the full CRUD lifecycle", async () => {
    const { client } = newMockServer((request) => {
      if (request.method === "POST" && request.path === "/channels") {
        return { status: 201, body: { id: "chan-1", channel: "telegram" } };
      }
      if (request.method === "GET" && request.path === "/channels/chan-1") {
        return { status: 200, body: { id: "chan-1", channel: "telegram" } };
      }
      if (request.method === "PATCH" && request.path === "/channels/chan-1") {
        return { status: 200, body: { id: "chan-1", channel: "telegram", is_active: false } };
      }
      if (request.method === "DELETE" && request.path === "/channels/chan-1") {
        return { status: 204 };
      }
      throw new Error(`unexpected request: ${request.method} ${request.path}`);
    });

    const created = await client.createChannel({
      channel: "telegram",
      name: "ops-alerts",
      config: { bot_token: "x", chat_id: "y" },
    });
    expect(created.id).toBe("chan-1");

    const fetched = await client.getChannel("chan-1");
    expect(fetched.id).toBe("chan-1");

    const updated = await client.updateChannel("chan-1", { is_active: false });
    expect(updated.is_active).toBe(false);

    await expect(client.deleteChannel("chan-1")).resolves.toBeUndefined();
  });
});

describe("api keys", () => {
  it("supports list, create, and revoke", async () => {
    const { client } = newMockServer((request) => {
      if (request.method === "GET" && request.path === "/keys") {
        return { status: 200, body: [{ id: "key-1", label: "ci" }] };
      }
      if (request.method === "POST" && request.path === "/keys") {
        return {
          status: 201,
          body: { key: { id: "key-2", label: "new" }, api_secret: "shown-once" },
        };
      }
      if (request.method === "DELETE" && request.path === "/keys/key-1") {
        return { status: 204 };
      }
      throw new Error(`unexpected request: ${request.method} ${request.path}`);
    });

    const keys = await client.listApiKeys();
    expect(keys).toHaveLength(1);

    const created = await client.createApiKey("new");
    expect(created.api_secret).toBe("shown-once");

    await expect(client.revokeApiKey("key-1")).resolves.toBeUndefined();
  });
});

describe("webhooks", () => {
  it("supports the full CRUD lifecycle", async () => {
    const { client } = newMockServer((request) => {
      if (request.method === "GET" && request.path === "/webhooks") {
        return { status: 200, body: [{ id: "wh-1" }] };
      }
      if (request.method === "POST" && request.path === "/webhooks") {
        return {
          status: 201,
          body: {
            id: "wh-2",
            url: "https://example.com/hook",
            events: ["notification.delivered"],
            secret: "whsec_shown_once",
          },
        };
      }
      if (request.method === "PUT" && request.path === "/webhooks/wh-1") {
        return { status: 200, body: { id: "wh-1", is_active: false } };
      }
      if (request.method === "DELETE" && request.path === "/webhooks/wh-1") {
        return { status: 204 };
      }
      throw new Error(`unexpected request: ${request.method} ${request.path}`);
    });

    const webhooks = await client.listWebhooks();
    expect(webhooks).toHaveLength(1);

    const created = await client.createWebhook({
      url: "https://example.com/hook",
      events: ["notification.delivered"],
    });
    expect(created.secret).toBe("whsec_shown_once");

    const updated = await client.updateWebhook("wh-1", { is_active: false });
    expect(updated.is_active).toBe(false);

    await expect(client.deleteWebhook("wh-1")).resolves.toBeUndefined();
  });
});
