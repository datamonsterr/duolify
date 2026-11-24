import { http, HttpResponse } from "msw";
import { expect, it } from "vitest";
import { authorization, connect, edge, go, network } from "../chat-fixture";

it("quotes, streams, stops and recovers a Tutor turn through WebSocket and SSE", async () => {
  authorization();
  const key = "stable-turn";
  const text = "An echo reflects sound.";
  const sent = {
    quote_id: "accepted-quote",
    idempotency_key: key,
    class: "short",
    message: "Explain echoes",
  };
  let engineCalls = 0;
  let finish: (() => void) | undefined;
  const delta = 'event: delta\ndata: {"text":"' + text + '"}\n\n';
  const stopped =
    'event: error\ndata: {"code":"stopped","interrupted":true}\n\n';
  network.use(
    http.post(go + "/quote", () => HttpResponse.json({ id: "accepted-quote" })),
    http.post(go + "/messages", async ({ request }) => {
      expect(await request.json()).toEqual(sent);
      const previous = engineCalls === 1;
      if (!previous) engineCalls++;
      const encoder = new TextEncoder();
      const stream = new ReadableStream({
        start(controller) {
          controller.enqueue(encoder.encode(": keepalive\n\n" + delta));
          finish = () => {
            controller.enqueue(encoder.encode(stopped));
            controller.close();
          };
          if (previous) finish();
        },
      });
      return new HttpResponse(stream, {
        headers: { "Content-Type": "text/event-stream" },
      });
    }),
    http.post(go + "/turns/" + key + "/stop", () => {
      finish?.();
      return HttpResponse.json({ state: "stopped" });
    }),
    http.get(go + "/turns/" + key, () =>
      HttpResponse.json({ state: "stopped", message: { content: text } }),
    ),
  );
  const { socket, replies } = await connect();
  try {
    socket.send(
      JSON.stringify({
        op: "quote",
        id: "quote",
        body: { class: "short", message: sent.message },
      }),
    );
    await expect
      .poll(() => replies.find((m) => m.id === "quote"))
      .toMatchObject({
        event: "result",
        status: 200,
        data: { id: "accepted-quote" },
      });
    socket.send(JSON.stringify({ op: "send", id: "send", body: sent }));
    await expect
      .poll(() => replies.find((m) => m.event === "delta"))
      .toMatchObject({ id: "send", data: { text } });
    expect(replies.some((m) => m.event === "heartbeat")).toBe(true);
    socket.send(JSON.stringify({ op: "stop", id: "stop", key }));
    await expect
      .poll(() => replies.find((m) => m.event === "error"))
      .toMatchObject({ id: "send", data: { code: "stopped" } });
    await expect
      .poll(() => replies.find((m) => m.id === "stop"))
      .toMatchObject({ event: "result", data: { state: "stopped" } });
  } finally {
    socket.close();
  }
  const reconnected = await connect();
  try {
    reconnected.socket.send(
      JSON.stringify({ op: "recover", id: "recover", key }),
    );
    await expect
      .poll(() => reconnected.replies.find((m) => m.id === "recover"))
      .toMatchObject({
        data: { state: "stopped", message: { content: text } },
      });
  } finally {
    reconnected.socket.close();
  }
  const recovery = await edge("/turns/" + key);
  expect(await recovery.json()).toEqual({
    state: "stopped",
    message: { content: text },
  });
  const sse = await edge("/messages", {
    method: "POST",
    body: JSON.stringify(sent),
  });
  expect(sse.headers.get("Content-Type")).toContain("text/event-stream");
  const wire = await sse.text();
  expect(wire).toContain(": keepalive");
  expect(wire).toContain("id: 1");
  expect(wire).toContain(delta);
  expect(wire).toContain(stopped);
  expect(engineCalls).toBe(1);
});

it("delivers an existing Guided turn and rejects the wrong Origin before dispatch", async () => {
  authorization({ course_id: "", thread_id: "", surface: "course_brief" });
  const turn = { current_step: "topic", answer: "Acoustics" };
  network.use(
    http.post(go + "/guided-turn", async ({ request }) => {
      expect(await request.json()).toEqual(turn);
      return HttpResponse.json({ next_step: "goal" });
    }),
  );
  const denied = await edge("/guided-turn", {
    method: "POST",
    body: JSON.stringify(turn),
    headers: { Origin: "https://different.example" },
  });
  expect(denied.status).toBe(403);
  const response = await edge("/guided-turn", {
    method: "POST",
    body: JSON.stringify(turn),
  });
  expect(response.status).toBe(200);
  expect(await response.json()).toEqual({ next_step: "goal" });
});
