import { defineConfig } from "@playwright/test";
import config from "./playwright.config";

export default defineConfig({
  ...config,
  testMatch: [
    "design-e13-03.spec.ts",
    "design-e13-04.spec.ts",
    "mascot-g5.spec.ts",
    "mascot-g9.spec.ts",
    "guided-script-e1.spec.ts",
    "guided-script-a11y.spec.ts",
    "copy-budget.spec.ts",
    "phone-layout.spec.ts",
    "keyboard-shortcuts.spec.ts",
    "characters-v2-creator.spec.ts",
    "characters-v2-kinds.spec.ts",
  ],
  // The authored fixture shares the built web server and needs no API process.
  webServer: Array.isArray(config.webServer)
    ? config.webServer[0]
    : config.webServer,
});
