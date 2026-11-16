import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

for (const width of [1280, 390]) {
  test.describe(`publisher birth date at ${width}px`, () => {
    test.use({ viewport: { width, height: 900 } });
    for (const adult of [true, false]) {
      test(`${adult ? "adult continues" : "minor is refused"} after the first-publish prompt`, async ({
        page,
        request,
      }) => {
        await page.goto("/profile");
        const token = await page.evaluate(
          () =>
            JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
              .access_token as string,
        );
        const headers = { Authorization: `Bearer ${token}` };
        const seeded = await request.post(`${apiBase}/test/sharing/course`, {
          headers,
          data: { variant: "safe", birth_date_unknown: true },
        });
        expect(seeded.status()).toBe(201);
        const { course_id } = await seeded.json();
        await page.route(
          /\/v1\/(courses|features|gamification|me)(\/|$|\?)/,
          async (route) => {
            const original = route.request();
            const target = new URL(original.url());
            target.host = new URL(apiBase).host;
            target.protocol = new URL(apiBase).protocol;
            const forwarded = original.headers();
            delete forwarded.origin;
            await route.fulfill({
              response: await request.fetch(target.href, {
                method: original.method(),
                headers: forwarded,
                data: original.postData() ?? undefined,
              }),
            });
          },
        );
        await page.goto(`/course/${course_id}`);
        await page.getByRole("button", { name: "Share course" }).click();
        const panel = page.getByRole("dialog", { name: "Share course" });
        await panel
          .getByRole("radio", { name: /Anyone with the link/ })
          .click();
        const birthDate = panel.getByLabel("Birth date", { exact: true });
        await expect(birthDate).toBeVisible();
        await expect(birthDate).toBeFocused();
        await expect(panel).toContainText("You must be 18 or older to publish");
        await birthDate.fill(adult ? "1990-06-15" : "2012-06-15");
        await panel.getByRole("button", { name: "Save and continue" }).click();
        if (adult) {
          await expect(panel.getByTestId("share-link-url")).toBeVisible();
          await panel
            .getByRole("button", { name: "Close", exact: true })
            .click();
          await page.getByRole("button", { name: "Share course" }).click();
          await expect(
            panel.getByLabel("Birth date", { exact: true }),
          ).toHaveCount(0);
        } else {
          await expect(panel.getByRole("alert")).toContainText(
            "You must be 18 or older to publish",
          );
          await expect(panel.getByTestId("share-link-url")).toHaveCount(0);
          await expect(birthDate).toHaveCount(0);
          await expect(
            panel.getByRole("button", { name: "Save and continue" }),
          ).toHaveCount(0);
          const overwrite = await request.put(`${apiBase}/v1/me/birth-date`, {
            headers,
            data: { birth_date: "1990-06-15" },
          });
          expect(overwrite.status()).toBe(409);
          expect(await overwrite.json()).toEqual({
            error: "birth_date_locked",
          });
          const stillRefused = await request.post(
            `${apiBase}/v1/courses/${course_id}/share-link`,
            {
              headers,
              data: {},
            },
          );
          expect(stillRefused.status()).toBe(403);
          expect(await stillRefused.json()).toEqual({
            error: "adult_required",
          });
          expect(
            (
              await request.get(
                `${apiBase}/v1/courses/${course_id}/share-link`,
                { headers },
              )
            ).ok(),
          ).toBe(true);
        }
        await expect(panel).not.toContainText(
          adult ? "1990-06-15" : "2012-06-15",
        );
      });
    }
  });
}
