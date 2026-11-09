// Shared by the browser server configuration and its real-API fixtures.
export const webPort = Number(process.env.E2E_WEB_PORT ?? 4184);
export const webBase = `http://127.0.0.1:${webPort}`;
export const apiBase = process.env.E2E_API_BASE ?? "http://127.0.0.1:18084";
