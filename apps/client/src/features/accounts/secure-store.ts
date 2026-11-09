import { invoke, isTauri } from "@tauri-apps/api/core";
export type Tokens = {
  access_token: string;
  refresh_token: string;
  expires_in: number;
};
export interface SecureStore {
  read(): Promise<Tokens | null>;
  write(value: Tokens | null): Promise<void>;
}
type NativeInvoke = (
  command: string,
  args?: Record<string, unknown>,
) => Promise<unknown>;
// Native errors propagate. Only browser builds use the disposable session fallback.
export function createSecureStore(native?: NativeInvoke): SecureStore {
  let memory: Tokens | null = null;
  return {
    async read() {
      if (native) {
        const raw = await native("read_credentials");
        return typeof raw === "string" ? (JSON.parse(raw) as Tokens) : null;
      }
      try {
        const raw = sessionStorage.getItem("duolify.auth");
        return raw ? (JSON.parse(raw) as Tokens) : memory;
      } catch {
        return memory;
      }
    },
    async write(value) {
      if (native) {
        await native("write_credentials", {
          value: value ? JSON.stringify(value) : null,
        });
        return;
      }
      memory = value;
      try {
        if (value)
          sessionStorage.setItem("duolify.auth", JSON.stringify(value));
        else sessionStorage.removeItem("duolify.auth");
      } catch {
        /* Memory remains available in restricted browser contexts. */
      }
    },
  };
}
export const secureStore = createSecureStore(isTauri() ? invoke : undefined);
