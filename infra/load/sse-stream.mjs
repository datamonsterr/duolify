// Tutor chat stream leg of the load baseline (E18-02). k6 2.x has no SSE/streaming-read API, so this
// Node script (Node is in the mise toolchain) drives the real flow through the API: quote, accept,
// POST /tutor/messages, then reads the stream incrementally and records the gap between any two
// received chunks. Fails (exit 1) on a gap >= MAX_GAP_SECONDS, a non-200, or a missing terminal event.
// Env: API_URL, ACCESS_TOKEN, COURSE_ID, SSE_SECONDS (expected minimum duration), MAX_GAP_SECONDS (25),
// SSE_RESULT=<file> to write the JSON result.
import { writeFileSync } from "node:fs";

const { API_URL, ACCESS_TOKEN, COURSE_ID } = process.env;
const expectSeconds = Number(process.env.SSE_SECONDS || 600);
const maxGap = Number(process.env.MAX_GAP_SECONDS || 25);
const headers = {
  "Content-Type": "application/json",
  Authorization: `Bearer ${ACCESS_TOKEN}`,
};
const post = (path, body, signal) =>
  fetch(`${API_URL}${path}`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
    signal,
  });

const message = "What is a moon?";
const q = await post(`/v1/courses/${COURSE_ID}/tutor/quote`, {
  class: "short",
  message,
});
const quote = await q.json();
if (!q.ok || !quote.id) throw new Error(`quote failed: ${q.status}`);
const acc = await post(`/v1/quotes/${quote.id}/accept`, {});
if (!acc.ok) throw new Error(`accept failed: ${acc.status}`);

const started = performance.now();
const res = await post(
  `/v1/courses/${COURSE_ID}/tutor/messages`,
  {
    quote_id: quote.id,
    idempotency_key: `load-${Date.now()}`,
    class: "short",
    message,
  },
  AbortSignal.timeout((expectSeconds + 120) * 1000),
);
let last = started;
let maxGapMs = 0;
let heartbeats = 0;
let text = "";
let firstByteMs = null;
const decoder = new TextDecoder();
let readError = null;
if (res.body) {
  try {
    for await (const chunk of res.body) {
      const now = performance.now();
      firstByteMs ??= now - started;
      maxGapMs = Math.max(maxGapMs, now - last);
      last = now;
      const s = decoder.decode(chunk, { stream: true });
      heartbeats += (s.match(/^:/gm) || []).length;
      text += s;
    }
  } catch (e) {
    readError = String(e.cause?.code || e.message);
  }
}
const totalMs = performance.now() - started;
const result = {
  status: res.status,
  duration_s: +(totalMs / 1000).toFixed(1),
  first_byte_ms: firstByteMs === null ? null : Math.round(firstByteMs),
  heartbeats,
  max_gap_s: +(maxGapMs / 1000).toFixed(2),
  max_gap_limit_s: maxGap,
  completed: text.includes("event: done"),
  read_error: readError,
  tail: text.slice(-200),
};
if (process.env.SSE_RESULT)
  writeFileSync(process.env.SSE_RESULT, JSON.stringify(result));
console.log(JSON.stringify(result));
const problems = [];
if (res.status !== 200) problems.push(`status ${res.status}`);
if (!result.completed) problems.push("no terminal done event");
if (maxGapMs / 1000 >= maxGap)
  problems.push(`max gap ${result.max_gap_s}s >= ${maxGap}s`);
if (totalMs / 1000 < expectSeconds * 0.9)
  problems.push(
    `stream ended after ${result.duration_s}s, expected ~${expectSeconds}s`,
  );
if (problems.length) {
  console.error(`FAIL: ${problems.join("; ")}`);
  process.exit(1);
}
