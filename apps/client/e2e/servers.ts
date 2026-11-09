// Shared by the browser server configuration and its real-API fixtures.
export const webPort = Number(process.env.DUOLIFY_E2E_WEB_PORT || 4184);
export const webBase = `http://127.0.0.1:${webPort}`;
export const apiBase =
  process.env.DUOLIFY_E2E_API_URL || "http://127.0.0.1:18084";
