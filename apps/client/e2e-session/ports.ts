export const webPort = Number(process.env.E2E_SESSION_WEB_PORT ?? 4180);
export const apiPort = Number(process.env.E2E_SESSION_API_PORT ?? 4181);
export const sessionWeb = `http://127.0.0.1:${webPort}`;
export const sessionApi = `http://127.0.0.1:${apiPort}`;
