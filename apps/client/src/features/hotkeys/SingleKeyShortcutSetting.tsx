import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { profileApi } from "../accounts/learner-context";
import { session } from "../accounts/session";
import { useUiPreferences } from "../design/localization/Preferences";
import { shortcutMessage } from "./messages";
import { useHotkeys } from "./useHotkeys";

function settingKey() {
  return ["single-key-shortcuts", session.current()?.id ?? ""] as const;
}

function useSingleKeyShortcuts() {
  const queryKey = settingKey();
  return useQuery({
    queryKey,
    enabled: !!queryKey[1],
    queryFn: async () =>
      (await profileApi.readSettings()).single_key_shortcuts !== false,
  });
}

/** Mount in Settings alongside the other Learner preferences. */
export function SingleKeyShortcutSetting() {
  const settings = useSingleKeyShortcuts();
  const queryClient = useQueryClient();
  const queryKey = settingKey();
  const { preferences } = useUiPreferences();
  const save = useMutation({
    mutationFn: async (value: boolean) =>
      (await profileApi.saveSettings({ single_key_shortcuts: value }))
        .single_key_shortcuts,
    onSuccess: (value) => queryClient.setQueryData(queryKey, value),
  });
  return (
    <>
      <label>
        <input
          type="checkbox"
          checked={settings.data ?? true}
          disabled={settings.isLoading || save.isPending}
          onChange={(event) => save.mutate(event.currentTarget.checked)}
        />
        {shortcutMessage(preferences.locale, "single_keys")}
      </label>
      {save.isError && (
        <p role="alert">{shortcutMessage(preferences.locale, "save_failed")}</p>
      )}
    </>
  );
}

/** Pass player handlers and mark the focusable player root with data-hotkey-player. */
export function usePlayerHotkeys(
  handlers: Parameters<typeof useHotkeys>[0]["handlers"],
) {
  const setting = useSingleKeyShortcuts();
  useHotkeys({
    scope: "player",
    surface: "lesson",
    handlers,
    singleKeyShortcuts: setting.data ?? true,
  });
}
