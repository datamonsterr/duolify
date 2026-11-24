import { exports } from "cloudflare:workers";
import { http, HttpResponse } from "msw";
import { expect } from "vitest";
import { network } from "./chat-network";

export { network };
export const origin = "https://client.example.test";
export const go = "https://go.example.test/internal/edge/chat";
export const path = "https://edge.example.test/v1/edge/chat";
export const token = "test-channel.signature";
export const protocol = "duolify.chat.v1";
export const scope = {
  learner_id: "00000000-0000-4000-8000-000000000001",
  session_id: "00000000-0000-4000-8000-000000000002",
  course_id: "00000000-0000-4000-8000-000000000003",
  thread_id: "00000000-0000-4000-8000-000000000004",
  origin,
  expires_at: 0,
};

export function authorization(overrides = {}) {
  network.use(
    http.post(go + "/authorize", async ({ request }) => {
      expect(request.headers.get("Cookie")).toBeNull();
      expect(request.headers.get("Authorization")).toMatch(/^Bearer /);
      const grant = await request.json();
      if (
        !grant ||
        typeof grant !== "object" ||
        !("token" in grant) ||
        (grant.token !== token && grant.token !== "guided.signature")
      ) {
        return new HttpResponse(null, { status: 401 });
      }
      return HttpResponse.json({
        ...scope,
        expires_at: Math.floor(Date.now() / 1000) + 300,
        ...overrides,
      });
    }),
  );
}

export function edge(suffix: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  headers.set("Origin", headers.get("Origin") ?? origin);
  if (!headers.has("Upgrade")) headers.set("Authorization", "Bearer " + token);
  return exports.default.fetch(
    new Request(path + suffix, { ...init, headers }),
  );
}

export async function connect() {
  const response = await edge("", {
    headers: {
      Upgrade: "websocket",
      "Sec-WebSocket-Protocol": protocol + ", " + token,
      Cookie: "ignore=learner-cookie",
    },
  });
  expect(response.status).toBe(101);
  expect(response.headers.get("Sec-WebSocket-Protocol")).toBe(protocol);
  const socket = response.webSocket!;
  const replies: Array<Record<string, unknown>> = [];
  socket.addEventListener("message", ({ data }) => {
    replies.push(JSON.parse(String(data)) as Record<string, unknown>);
  });
  socket.accept();
  return { socket, replies };
}
