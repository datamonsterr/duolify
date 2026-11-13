import { test as base, expect } from "@playwright/test";

// Tests that exercise reload/offline persistence use this explicit native
// credential-store double. The actual browser fallback never persists tokens.
export const test = base.extend<{ nativeCredentialStore: void }>({
  nativeCredentialStore: [
    async ({ context }, use) => {
      await context.addInitScript(() => {
        Object.defineProperty(window, "__TAURI_INTERNALS__", {
          configurable: true,
          value: {
            invoke: async (
              command: string,
              args?: { value?: string | null },
            ) => {
              const key = "duolify.native-store-fixture";
              if (command === "read_credentials")
                return sessionStorage.getItem(key);
              if (command === "write_credentials") {
                if (args?.value) sessionStorage.setItem(key, args.value);
                else sessionStorage.removeItem(key);
                return;
              }
              throw new Error(`Unexpected native command: ${command}`);
            },
          },
        });
        Object.defineProperty(window, "isTauri", {
          configurable: true,
          value: true,
        });
      });
      await use();
    },
    { auto: true },
  ],
});
export { expect };
export type { APIRequestContext, Page } from "@playwright/test";
