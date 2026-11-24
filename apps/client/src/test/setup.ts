import "@testing-library/jest-dom/vitest";
import { setupServer } from "msw/node";
import { beforeAll, afterAll, afterEach } from "vitest";
import { cleanup } from "@testing-library/react";
import { handlers } from "../mocks/handlers";
import { loadArabic } from "../features/design/localization/messages";
export const server = setupServer(...handlers);
beforeAll(async () => {
  server.listen({ onUnhandledFrame: "error" });
  await loadArabic(); // tests render Arabic synchronously
});
afterEach(() => {
  server.resetHandlers();
  cleanup();
});
afterAll(() => server.close());
