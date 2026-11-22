// Test inbox for the staging flow suite: Cloudflare Email Routing hands every message sent to
// *@test.duolify.online to email(); the six-digit sign-in codes are kept per recipient in KV for a
// few minutes. GET /codes?to=<address> (Authorization: Bearer MAILBOX_TOKEN) lists them, oldest first.
const TTL_SECONDS = 600;
const MAX_CODES = 20;

// Message text with quoted-printable soft breaks and escapes undone, so a code split by a line wrap is found.
export function messageText(raw) {
  return raw
    .replace(/=\r?\n/g, "")
    .replace(/=([0-9A-F]{2})/g, (_, h) => String.fromCharCode(parseInt(h, 16)));
}

export function extractCodes(raw) {
  const split = raw.search(/\r?\n\r?\n/); // skip the headers: message ids hold digits
  const body = split < 0 ? raw : raw.slice(split);
  const text = messageText(body).replace(/<[^>]*>/g, " ");
  return [...new Set(text.match(/(?<![\d.#])\d{6}(?!\d)/g) ?? [])];
}

export default {
  async email(message, env) {
    const to = message.to.toLowerCase();
    const codes = extractCodes(await new Response(message.raw).text());
    if (codes.length === 0) return;
    const kept = JSON.parse((await env.MAILBOX.get(to)) ?? "[]");
    await env.MAILBOX.put(to, JSON.stringify([...kept, ...codes].slice(-MAX_CODES)), {
      expirationTtl: TTL_SECONDS,
    });
  },

  async fetch(request, env) {
    if (!env.MAILBOX_TOKEN || request.headers.get("Authorization") !== `Bearer ${env.MAILBOX_TOKEN}`) {
      return new Response("unauthorized", { status: 401 });
    }
    const to = new URL(request.url).searchParams.get("to")?.toLowerCase().replaceAll(" ", "+"); // a raw "+" in a query reads as a space
    if (!to) return new Response("missing ?to=", { status: 400 });
    const codes = JSON.parse((await env.MAILBOX.get(to)) ?? "[]");
    return new Response(codes.map((code) => `${code}\n`).join(""), {
      headers: { "content-type": "text/plain" },
    });
  },
};
