import { defineConfig } from "@playwright/test";
import config from "./playwright.config";

export default defineConfig({
  ...config,
  testMatch: ["design-e13-03.spec.ts", "design-e13-04.spec.ts"],
  // The authored fixture shares the built web server and needs no API process.
  webServer: Array.isArray(config.webServer)
    ? config.webServer[0]
    : config.webServer,
});
