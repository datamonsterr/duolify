import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

// The old forms stay one release behind the `rest` flag; this spec covers them.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() =>
    localStorage.setItem("duolify.guided.rest", "off"),
  );
});

test("published Characters survive edits, confirmed forks, deletion and takedown replay", async ({
  page,
  request,
}) => {
  await page.goto("/profile");
  const ownerTokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  const owner = { Authorization: `Bearer ${ownerTokens.access_token}` };
  const seeded = await request.post(`${apiBase}/test/sharing/course`, {
    headers: owner,
    data: { variant: "reuse" },
  });
  expect(seeded.status()).toBe(201);
  const { course_id: course } = await seeded.json();
  const charSeed = await request.post(`${apiBase}/test/characters`, {
    headers: owner,
  });
  expect(charSeed.ok()).toBeTruthy();
  const { character_id: id } = await charSeed.json();
  expect(
    (
      await request.put(`${apiBase}/v1/courses/${course}/cast`, {
        headers: owner,
        data: { guide: id, supporting: [] },
      })
    ).ok(),
  ).toBeTruthy();
  expect(
    (
      await request.post(`${apiBase}/test/characters/publication-cast`, {
        headers: owner,
        data: { course_id: course },
      })
    ).status(),
  ).toBe(204);
  const draft = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers: owner, data: { character_export_confirmed: true } },
  );
  expect(draft.status()).toBe(201);
  expect((await draft.json()).verdict).toBe("pass");
  const link = await request.post(
    `${apiBase}/v1/courses/${course}/share-link`,
    { headers: owner, data: {} },
  );
  expect(link.status()).toBe(201);
  const { token } = await link.json();
  const email = `snapshot-${crypto.randomUUID()}@example.test`;
  await request.post(`${apiBase}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const learnerTokens = await (
    await request.post(`${apiBase}/v1/auth/verify`, {
      data: { email, code, age_confirmed: true },
    })
  ).json();
  const learner = { Authorization: `Bearer ${learnerTokens.access_token}` };
  const enrolled = await request.post(
    `${apiBase}/v1/public/shares/${token}/enroll`,
    { headers: learner, data: {} },
  );
  expect(enrolled.status()).toBe(201);
  const { course_id: copy } = await enrolled.json();
  const state = await (
    await request.get(`${apiBase}/v1/courses/${copy}`, { headers: learner })
  ).json();
  const lessons = state.units.flatMap(
    (u: { lessons: { id: string }[] }) => u.lessons,
  );
  const manifest = async () => {
    const response = await request.get(
      `${apiBase}/v1/lessons/${lessons[0].id}/manifest`,
      { headers: learner },
    );
    expect(response.status()).toBe(200);
    return response.json();
  };
  const first = await manifest();
  expect(first.cast.speakers.guide.character_id).toMatch(
    /^snapshot-[0-9a-f]{64}$/,
  );
  expect(first.cast.speakers.guide.spec.info.name).toBe("Seed guide");
  const original = await (
    await request.get(`${apiBase}/v1/characters/${id}`, { headers: owner })
  ).json();
  original.spec.info.name = "Edited source guide";
  expect(
    (
      await request.patch(`${apiBase}/v1/characters/${id}`, {
        headers: owner,
        data: { expected_version: 1, spec: original.spec },
      })
    ).status(),
  ).toBe(200);
  const republished = await request.post(
    `${apiBase}/v1/courses/${course}/republish`,
    {
      headers: owner,
      data: { changelog: "New future guide", character_export_confirmed: true },
    },
  );
  expect(republished.status()).toBe(201);
  expect((await republished.json()).verdict).toBe("pass");
  expect((await manifest()).cast).toEqual(first.cast);
  const update = await (
    await request.get(`${apiBase}/v1/courses/${copy}/update`, {
      headers: learner,
    })
  ).json();
  expect(update.update_available).toBe(true);
  expect(
    (
      await request.put(`${apiBase}/v1/courses/${copy}/cast`, {
        headers: learner,
        data: { guide: "default-guide", supporting: [] },
      })
    ).status(),
  ).toBe(409);
  // On-demand generation beyond the frozen Lessons retains the published guide and existing quote.
  for (const lesson of lessons.slice(0, 2)) {
    const test = await (
      await request.get(`${apiBase}/v1/lessons/${lesson.id}/test`, {
        headers: learner,
      })
    ).json();
    for (const question of test.questions)
      expect(
        (
          await request.post(`${apiBase}/v1/lessons/${lesson.id}/test/answer`, {
            headers: learner,
            data: { question_id: question.question_id, choice_index: 0 },
          })
        ).ok(),
      ).toBeTruthy();
  }
  expect(
    (
      await request.post(`${apiBase}/test/sharing/fund`, {
        headers: learner,
        data: { credits: 1000 },
      })
    ).status(),
  ).toBe(204);
  const quote = await (
    await request.post(`${apiBase}/v1/courses/${copy}/missing-lessons/quote`, {
      headers: learner,
      data: {},
    })
  ).json();
  const generated = await request.post(
    `${apiBase}/v1/courses/${copy}/missing-lessons/generate`,
    {
      headers: learner,
      data: {
        lesson_id: quote.lesson_id,
        quote_id: quote.quote_id,
        consent: true,
      },
    },
  );
  expect(generated.status()).toBe(202);
  await expect
    .poll(async () => {
      const got = await request.get(
        `${apiBase}/v1/courses/${copy}/missing-lessons`,
        { headers: learner },
      );
      return (await got.json()).state;
    })
    .toBe("complete");
  const beyond = await (
    await request.get(`${apiBase}/v1/lessons/${lessons[2].id}/manifest`, {
      headers: learner,
    })
  ).json();
  expect(beyond.cast.speakers.guide.character_id).toBe(
    first.cast.speakers.guide.character_id,
  );
  const instruction = {
    text: "More concrete examples",
    scope: "rest_of_course",
  };
  expect(
    (
      await request.post(`${apiBase}/v1/courses/${copy}/instructions`, {
        headers: learner,
        data: instruction,
      })
    ).status(),
  ).toBe(409);
  expect(
    (
      await request.post(`${apiBase}/v1/courses/${copy}/instructions`, {
        headers: learner,
        data: { ...instruction, confirm_fork: true },
      })
    ).status(),
  ).toBe(201);
  expect(
    (
      await request.put(`${apiBase}/v1/courses/${copy}/cast`, {
        headers: learner,
        data: { guide: "default-companion", supporting: [] },
      })
    ).status(),
  ).toBe(200);
  expect((await manifest()).cast).toEqual(first.cast);
  expect(
    (
      await request.delete(`${apiBase}/v1/characters/${id}`, { headers: owner })
    ).ok(),
  ).toBeTruthy();
  expect(
    (
      await request.post(`${apiBase}/v1/courses/${course}/unpublish`, {
        headers: owner,
        data: {},
      })
    ).status(),
  ).toBe(204);
  expect((await manifest()).cast).toEqual(first.cast);
  expect(
    (
      await request.post(`${apiBase}/v1/me/deletion-request`, {
        headers: owner,
        data: { confirmation: "DELETE" },
      })
    ).status(),
  ).toBe(200);
  expect((await manifest()).cast).toEqual(first.cast);
  await request.post(`${apiBase}/test/moderation/grant`, { headers: learner });
  expect(
    (
      await request.post(
        `${apiBase}/v1/moderation/character-snapshots/${first.cast.speakers.guide.character_id}/takedown`,
        { headers: learner, data: { reason_code: "unsafe" } },
      )
    ).status(),
  ).toBe(204);
  const suppressed = await manifest();
  expect(suppressed.cast.speakers.guide.character_id).toBe("default-guide");
  expect(suppressed.cast.fallback_used).toBe(true);
  const restored = await request.post(`${apiBase}/test/sharing/restore`, {
    headers: learner,
    data: { rewind_blocks: true },
  });
  expect(restored.status()).toBe(200);
  expect((await manifest()).cast.speakers.guide.character_id).toBe(
    "default-guide",
  );
});

for (const width of [1280, 390]) {
  test.describe(`Character disclosure ${width}`, () => {
    test.use({ viewport: { width, height: 900 }, hasTouch: width === 390 });
    test(`Character export requires informed confirmation at ${width}px`, async ({
      page,
      request,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({
        reducedMotion: "reduce",
        colorScheme: width === 390 ? "dark" : "light",
      });
      await page.goto("/profile");
      const tokens = await page.evaluate(() =>
        JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
      );
      const headers = { Authorization: `Bearer ${tokens.access_token}` };
      const seeded = await request.post(`${apiBase}/test/sharing/course`, {
        headers,
        data: { variant: "safe" },
      });
      expect(seeded.status()).toBe(201);
      const { course_id } = await seeded.json();
      expect(
        (
          await request.put(`${apiBase}/v1/courses/${course_id}/cast`, {
            headers,
            data: { guide: "default-guide", supporting: [] },
          })
        ).ok(),
      ).toBeTruthy();
      await page.route(
        /\/v1\/(courses|features|gamification)(\/|$|\?)/,
        async (route) => {
          const original = route.request();
          const target = new URL(original.url());
          target.host = new URL(apiBase).host;
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
      let builds = 0;
      page.on("request", (r) => {
        if (r.method() === "POST" && r.url().endsWith("/publication-drafts"))
          builds++;
      });
      await page.goto(`/course/${course_id}`);
      const opener = page.getByRole("button", {
        name: "Preview publication draft",
      });
      await opener.click();
      const dialog = page.getByRole("dialog", {
        name: "Preview publication draft",
      });
      await expect(
        dialog.getByRole("heading", {
          name: "Characters travel with this Course",
        }),
      ).toBeVisible();
      await expect(dialog).toContainText(
        "name, bio, backstory, speaking style, persona and appearance",
      );
      expect(builds).toBe(0);
      const confirm = dialog.getByRole("button", {
        name: "Confirm Character export",
      });
      await expect(confirm).toBeDisabled();
      const consent = dialog.getByRole("checkbox", {
        name: "I confirm these Characters can become Course content",
      });
      if (width === 390) await consent.tap();
      else {
        await consent.focus();
        await page.keyboard.press("Space");
      }
      await confirm.click();
      await expect(dialog.getByTestId("draft-verdict")).toBeVisible();
      expect(builds).toBe(1);
      await page.keyboard.press("Escape");
      await expect(opener).toBeFocused();
      await page
        .getByRole("button", { name: "Share course", exact: true })
        .click();
      const share = page.getByRole("dialog", {
        name: "Share course",
        exact: true,
      });
      const linkVisibility = share.getByRole("radio", {
        name: /Anyone with the link/,
      });
      await linkVisibility.click();
      await expect(
        share.getByRole("heading", {
          name: "Characters travel with this Course",
        }),
      ).toBeVisible();
      await expect(linkVisibility).not.toBeChecked();
      await expect(
        share.getByRole("radio", { name: "Private", exact: true }),
      ).toBeChecked();
      await share
        .getByRole("checkbox", {
          name: "I confirm these Characters can become Course content",
        })
        .check();
      await share
        .getByRole("button", { name: "Confirm Character export" })
        .click();
      await expect(share.getByTestId("share-link-url")).toBeVisible();
      await expect(linkVisibility).toBeChecked();
      await share.getByRole("button", { name: "Close", exact: true }).click();
      const refused = await request.post(
        `${apiBase}/v1/courses/${course_id}/publication-drafts`,
        { headers, data: {} },
      );
      expect(refused.status()).toBe(409);
      expect(await refused.json()).toMatchObject({
        error: "character_export_confirmation_required",
      });
    });
  });
}
