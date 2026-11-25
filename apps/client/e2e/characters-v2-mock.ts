import type { Page } from "./native-store-fixture";

export const v1 = (name: string) => ({
  schema_version: 1,
  rig_version: "original-v1",
  info: { name, bio: "", backstory: "" },
  personality: {
    traits: ["kind"],
    speaking_style: "Clear",
    persona_prompt: "Help",
  },
  appearance: {
    body_shape: "round",
    palette: { body: "#FFD23F", accent: "#EE4266", outline: "#1A1A24" },
    eyes: "round",
    mouth: "smile",
    accessories: [],
  },
  expressions: ["idle"],
  voice_key: "voice-provisional-guide",
});

export async function mockCharacters(page: Page) {
  const created: unknown[] = [];
  const spends: string[] = [];
  page.on("request", (r) => {
    if (
      /\/v1\/(credits|characters\/quote|billing)/.test(r.url()) &&
      r.method() !== "GET"
    )
      spends.push(r.method() + " " + r.url());
  });
  const patched: unknown[] = [];
  const records: Record<string, unknown>[] = [];
  await page.addInitScript(() =>
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify({
        access_token: "a",
        refresh_token: "r",
        expires_in: 900,
      }),
    ),
  );
  await page.route("**/v1/**", (route) => route.fulfill({ json: [] }));
  await page.route("**/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "10000000-0000-4000-8000-000000000001",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: true,
        welcome_state: "completed",
        tour_state: "completed",
      },
    }),
  );
  await page.route(/\/v1\/characters(\/[^/]+)?$/, async (route) => {
    const req = route.request();
    if (req.method() === "GET") return route.fulfill({ json: records });
    const body = req.postDataJSON() as { spec: { info: { name: string } } };
    if (req.method() === "POST") {
      created.push(body.spec);
      const rec = {
        id: "c1",
        version: 1,
        spec: v1(body.spec.info.name),
        document: body.spec,
      };
      records.push(rec);
      return route.fulfill({ status: 201, json: rec });
    }
    patched.push(body);
    const rec = {
      id: "c1",
      version: 2,
      spec: v1(body.spec.info.name),
      document: body.spec,
    };
    records[0] = rec;
    return route.fulfill({ json: rec });
  });
  return { created, patched, spends };
}
