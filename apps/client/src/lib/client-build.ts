import packageInfo from "../../package.json";

export const appVersion = packageInfo.version;

export function clientPlatform(
  native: boolean,
  mobileTarget: string | undefined,
): "web" | "desktop" | "ios" | "android" {
  if (!native) return "web";
  if (mobileTarget === "ios" || mobileTarget === "android") return mobileTarget;
  return "desktop";
}
