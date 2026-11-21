// k6 load baseline (E18-02): 1k-DAU read mix + a long Tutor chat SSE stream against the mock Engine.
// k6 core has no streaming/SSE API, so the stream is one long buffered request; heartbeats are
// counted from the body and the "done" event is the completion signal. All knobs are env vars.
import http from "k6/http";
import { check } from "k6";
import { Counter } from "k6/metrics";

const API = __ENV.API_URL || "http://127.0.0.1:8080";
const MOCK = __ENV.MOCK_ENGINE_URL || "http://127.0.0.1:8090";
const SSE_SECONDS = Number(__ENV.SSE_SECONDS || 600);
const SSE_HEARTBEAT = Number(__ENV.SSE_HEARTBEAT_SECONDS || 20);
// 1k DAU at ~40 reads/day with a 10x peak factor is ~5 requests/s.
const READ_RPS = Number(__ENV.READ_RPS || 5);
const READ_DURATION = __ENV.READ_DURATION || "10m";

const readRequests = new Counter("read_requests");
const sseHeartbeats = new Counter("sse_heartbeats");
const sseDone = new Counter("sse_done");

// Public read endpoints, weighted: [path, weight].
const MIX = [
  ["/v1/courses/shared-demo", 4],
  ["/v1/pricing", 2],
  ["/v1/features", 2],
  ["/v1/characters/default-cast", 1],
  ["/healthz", 1],
];
const total = MIX.reduce((n, [, w]) => n + w, 0);

export const options = {
  scenarios: {
    reads: {
      executor: "constant-arrival-rate",
      rate: READ_RPS,
      timeUnit: "1s",
      duration: READ_DURATION,
      preAllocatedVUs: 10,
      maxVUs: 100,
      exec: "reads",
    },
    tutor_sse: {
      executor: "per-vu-iterations",
      vus: 1,
      iterations: 1,
      maxDuration: `${SSE_SECONDS + 120}s`,
      exec: "tutorSSE",
    },
  },
  thresholds: {
    "http_req_failed{scenario:reads}": ["rate<0.01"],
    "http_req_duration{scenario:reads}": ["p(95)<1000"],
    sse_done: ["count==1"],
    checks: ["rate==1"],
  },
  summaryTrendStats: ["avg", "p(90)", "p(95)", "max"],
};

function pick() {
  let r = Math.random() * total;
  for (const [path, w] of MIX) if ((r -= w) < 0) return path;
  return MIX[0][0];
}

export function reads() {
  const res = http.get(`${API}${pick()}`);
  readRequests.add(1);
  check(res, { "read 200": (r) => r.status === 200 });
}

export function tutorSSE() {
  const res = http.get(`${MOCK}/stream?seconds=${SSE_SECONDS}&heartbeat=${SSE_HEARTBEAT}`, {
    headers: { Accept: "text/event-stream" },
    timeout: `${SSE_SECONDS + 60}s`,
    tags: { name: "tutor_sse" },
  });
  const body = res.body || "";
  const beats = body.split(": heartbeat").length - 1;
  const done = body.includes("event: done");
  sseHeartbeats.add(beats);
  if (done) sseDone.add(1);
  check(res, {
    "sse 200 (no 524)": (r) => r.status === 200,
    "sse finished with done": () => done,
    "sse heartbeat at least every 25s": () => beats >= Math.floor(SSE_SECONDS / 25),
  });
}
