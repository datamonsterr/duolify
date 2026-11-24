import type { ComponentProps } from "react";
import {
  UiPreferencesProvider,
  type PreferenceAdapter,
} from "../design/localization/Preferences";
import { resolveLocale } from "../design/localization/messages";
import { Landing } from "./Landing";

export function LandingShell(props: ComponentProps<typeof Landing>) {
  return (
    <UiPreferencesProvider
      adapter={landingPreferences}
      initialPreferences={{ locale: resolveLocale(navigator.language) }}
    >
      <Landing {...props} />
    </UiPreferencesProvider>
  );
}
const landingPreferences: PreferenceAdapter = {
  load: () => {
    try {
      return JSON.parse(
        localStorage.getItem("duolify.landing-prefs") ?? "null",
      );
    } catch {
      return null;
    }
  },
  save: (value: unknown) => {
    try {
      localStorage.setItem("duolify.landing-prefs", JSON.stringify(value));
    } catch {
      /* The choice stays for this visit. */
    }
  },
  clear: () => {
    try {
      localStorage.removeItem("duolify.landing-prefs");
    } catch {
      /* Nothing stored. */
    }
  },
};
