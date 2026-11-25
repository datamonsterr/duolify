import {
  evictDurableObject,
  listDurableObjectIds,
  runInDurableObject,
} from "cloudflare:test";
import { env } from "cloudflare:workers";
import { http, HttpResponse } from "msw";
import { expect, it } from "vitest";
import { authorization, connect, edge, go, network } from "../chat-fixture";

it("wakes a hibernated socket with its scoped authorization and keeps learner content out of storage", async () => {
  authorization();
  const privateText = "private learner content";
  const ownedTurn = "opaque-stable-turn";
  let erased = false;
  network.use(
    http.get(go + "/turns/" + ownedTurn, () =>
      erased
        ? HttpResponse.json({ error: "unauthorized" }, { status: 401 })
        : HttpResponse.json({
            state: "completed",
            message: { content: privateText },
          }),
    ),
  );
  const { socket, replies } = await connect();
  try {
    const binding = (env as unknown as { ChatSession: DurableObjectNamespace })
      .ChatSession;
    const ids = await listDurableObjectIds(binding);
    expect(ids).toHaveLength(1);
    const stub = binding.get(ids[0]);
    await evictDurableObject(stub);
    socket.send(
      JSON.stringify({ op: "recover", id: "after-wake", key: ownedTurn }),
    );
    await expect
      .poll(() => replies.find((m) => m.id === "after-wake"))
      .toMatchObject({
        data: { state: "completed", message: { content: privateText } },
      });
    const snapshot = await runInDurableObject(stub, async (_agent, state) => ({
      attachment: state.getWebSockets()[0].deserializeAttachment(),
      storage: [...(await state.storage.list()).entries()],
      tables: state.storage.sql
        .exec<{ name: string }>(
          "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT GLOB 'sqlite_*' AND name NOT GLOB '_cf_*'",
        )
        .toArray()
        .map(({ name }) => ({
          name,
          rows: state.storage.sql
            .exec('SELECT * FROM "' + name.replaceAll('"', '""') + '"')
            .toArray(),
        })),
    }));
    const stored = JSON.stringify(snapshot);
    expect(stored).not.toContain(privateText);
    expect(stored).not.toContain(ownedTurn);
    expect(stored).not.toContain("test-channel.signature");
    expect(stored).not.toContain("credit");
    erased = true;
    const closed = new Promise<number>((resolve) => {
      socket.addEventListener("close", ({ code }) => resolve(code), {
        once: true,
      });
    });
    socket.send(
      JSON.stringify({ op: "recover", id: "after-erasure", key: ownedTurn }),
    );
    await expect(closed).resolves.toBe(1008);
    const connections = await runInDurableObject(
      stub,
      (_agent, state) => state.getWebSockets().length,
    );
    expect(connections).toBe(0);
  } finally {
    socket.close();
  }
});

it("refuses an expired authorization before creating a new object", async () => {
  authorization({ expires_at: Math.floor(Date.now() / 1000) - 1 });
  const response = await edge("/thread");
  expect(response.status).toBe(401);
  expect(await response.json()).toEqual({ error: "unauthorized" });
});
