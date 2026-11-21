// Mock Tutor chat SSE surface for the load baseline. No provider; deterministic.
// GET /stream?seconds=N&heartbeat=H streams for N seconds, a ": heartbeat" comment every H
// seconds and an occasional token event, then "event: done".
import http from "node:http";

http
  .createServer((req, res) => {
    const url = new URL(req.url, "http://x");
    if (url.pathname === "/healthz") return res.end("ok");
    if (url.pathname !== "/stream") return res.writeHead(404).end();
    const seconds = Number(url.searchParams.get("seconds") ?? 600);
    const heartbeat = Number(url.searchParams.get("heartbeat") ?? 20);
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-store", "X-Accel-Buffering": "no" });
    let beats = 0;
    res.write(`event: token\ndata: {"text":"hello"}\n\n`);
    const beat = setInterval(() => {
      res.write(": heartbeat\n\n");
      if (beats++ % 6 === 0) res.write(`event: token\ndata: {"text":"..."}\n\n`);
    }, heartbeat * 1000);
    const end = setTimeout(() => {
      clearInterval(beat);
      res.end(`event: done\ndata: {"finish":"stop"}\n\n`);
    }, seconds * 1000);
    req.on("close", () => {
      clearInterval(beat);
      clearTimeout(end);
    });
  })
  .listen(8090);
