import assert from "node:assert/strict";
import test from "node:test";
import worker, { extractCodes } from "./worker.js";

const kv = () => {
  const m = new Map();
  return { get: async (k) => m.get(k) ?? null, put: async (k, v) => void m.set(k, v) };
};
const mail = (to, text) => ({
  to,
  raw: new Response(`Message-ID: <123456789012@x>\r\nSubject: code\r\n\r\n${text}`).body,
});

test("finds the code in plain, quoted-printable and html bodies, not in headers", () => {
  assert.deepEqual(extractCodes("Message-ID: <111111@x>\n\nYour code is 482913."), ["482913"]);
  assert.deepEqual(extractCodes("H: a\n\n<p>Code: <b>48=\n2913</b></p>"), ["482913"]);
  assert.deepEqual(extractCodes("H: a\n\nphone 1234567"), []);
});

test("stores codes per recipient and serves them oldest first behind the token", async () => {
  const env = { MAILBOX: kv(), MAILBOX_TOKEN: "t" };
  await worker.email(mail("E2E+a@test.duolify.online", "Code 111222"), env);
  await worker.email(mail("e2e+a@test.duolify.online", "Code 333444"), env);
  await worker.email(mail("e2e+b@test.duolify.online", "Code 555666"), env);
  const get = (to, auth = "Bearer t") =>
    worker.fetch(
      new Request(`https://x/codes?to=${to}`, { headers: auth ? { Authorization: auth } : {} }),
      env,
    );
  assert.equal(await (await get("e2e+a@test.duolify.online")).text(), "111222\n333444\n");
  assert.equal(await (await get("e2e+b@test.duolify.online")).text(), "555666\n");
  assert.equal((await get("e2e+a@test.duolify.online", "Bearer no")).status, 401);
  assert.equal((await get("e2e+a@test.duolify.online", "")).status, 401);
});
