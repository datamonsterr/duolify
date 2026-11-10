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
// Native errors propagate. Browser credentials live only in this store instance.
export function createSecureStore(native?: NativeInvoke): SecureStore {
  let memory: Tokens | null = null;
  return {
    async read() {
      if (native) {
        const raw = await native("read_credentials");
        return typeof raw === "string" ? (JSON.parse(raw) as Tokens) : null;
      }
      return memory;
    },
    async write(value) {
      if (native) {
        await native("write_credentials", {
          value: value ? JSON.stringify(value) : null,
        });
        return;
      }
      memory = value;
    },
  };
}
export const secureStore = createSecureStore(isTauri() ? invoke : undefined);
