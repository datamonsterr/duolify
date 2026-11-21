// k6 load baseline (E18-02): the 1k-DAU read mix, public and authenticated, against the API.
// The Tutor chat stream leg is infra/load/sse-stream.mjs (k6 2.x has no SSE/streaming-read API);
// run.sh runs both at once. Env: API_URL, ACCESS_TOKEN, COURSE_ID, LESSON_ID, READ_RPS, READ_DURATION.
import http from "k6/http";
import { check } from "k6";
import { Counter } from "k6/metrics";

const API = __ENV.API_URL || "http://127.0.0.1:8080";
const TOKEN = __ENV.ACCESS_TOKEN;
const COURSE = __ENV.COURSE_ID;
const LESSON = __ENV.LESSON_ID;
// 1k DAU at ~40 reads/day with a 10x peak factor is ~5 requests/s.
const READ_RPS = Number(__ENV.READ_RPS || 5);
const READ_DURATION = __ENV.READ_DURATION || "10m";

const readRequests = new Counter("read_requests");
const authedRequests = new Counter("authed_read_requests");

// [path, weight, authenticated]. A daily learner opens the app (me, course list), the learn path
// (course, lesson manifest), checks streak and credits; anonymous traffic hits the public pages.
// Documented in docs/infra/baseline.md; keep both in step.
const MIX = [
  ["/v1/courses", 15, true],
  [`/v1/courses/${COURSE}`, 20, true],
  [`/v1/lessons/${LESSON}/manifest`, 20, true],
  ["/v1/gamification/streak", 15, true],
  ["/v1/me", 8, true],
  ["/v1/me/credits", 7, true],
  ["/v1/courses/shared-demo", 5, false],
  ["/v1/pricing", 4, false],
  ["/v1/features", 3, false],
  ["/v1/characters/default-cast", 2, false],
  ["/healthz", 1, false],
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
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<1000"],
    checks: ["rate==1"],
  },
  summaryTrendStats: ["avg", "p(90)", "p(95)", "p(99)", "max"],
};

function pick() {
  let r = Math.random() * total;
  for (const m of MIX) if ((r -= m[1]) < 0) return m;
  return MIX[0];
}

export function reads() {
  const [path, , authed] = pick();
  const res = http.get(`${API}${path}`, {
    headers: authed ? { Authorization: `Bearer ${TOKEN}` } : {},
    tags: { name: path.replace(/[0-9a-f-]{36}/g, ":id") },
  });
  readRequests.add(1);
  if (authed) authedRequests.add(1);
  check(res, { "read 200": (r) => r.status === 200 });
}
