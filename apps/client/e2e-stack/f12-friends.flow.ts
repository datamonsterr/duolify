import type { Page } from "@playwright/test";
import { expect, signUp, test, uniqueEmail, type Flow } from "./stack";

test.use({ flowId: "F12" });

async function claimUsername(page: Page, username: string, flow?: Flow) {
  await page.goto("/friends");
  await expect(
    page.getByRole("heading", { name: "Friends", level: 1 }),
  ).toBeVisible();
  await page.getByRole("heading", { name: "Choose your username" }).waitFor();
  await page.getByLabel("Username", { exact: true }).fill(username);
  await flow?.step("choose username");
  await page.getByRole("button", { name: "Claim username" }).click();
  await expect(page.getByText(`@${username}`).first()).toBeVisible();
}

test(
  "F12 friends: usernames, request, accept, friend streak",
  {
    annotation: {
      type: "flow",
      description: "F12 Friends: username, request, accept, friend streak",
    },
  },
  async ({ page, browser, flow }) => {
    // The old username form and add-friend dialog are what this flow covers.
    const off = () => localStorage.setItem("duolify.guided.rest", "off");
    await page.addInitScript(off);
    const suffix = Date.now().toString(36).slice(-6);
    const ana = `ana${suffix}`;
    const ben = `ben${suffix}`;

    // Ben signs up in a second, independent browser context.
    const benContext = await browser.newContext();
    const benPage = await benContext.newPage();
    await benPage.addInitScript(off);
    try {
      await signUp(benPage, uniqueEmail("f12b"), undefined, "/friends");
      await claimUsername(benPage, ben);

      await signUp(page, uniqueEmail("f12a"), undefined, "/friends");
      await claimUsername(page, ana, flow);
      await flow.step("username claimed");

      await page.getByRole("button", { name: /^Add/ }).first().click();
      const add = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: "Add a friend" }),
      });
      await add.getByLabel("Username").fill(ben);
      const send = add.getByRole("button", { name: `Send request to ${ben}` });
      await expect(send).toBeVisible();
      await flow.step("friend found");
      await send.click();
      await expect(add.getByText("Request sent")).toBeVisible();
      await flow.step("request sent");
      await add.getByRole("button", { name: "Close" }).click();

      await benPage.reload();
      await benPage.getByRole("tab", { name: /^Requests/ }).click();
      const accept = benPage.getByRole("button", { name: `Accept ${ana}` });
      await expect(accept).toBeVisible();
      await flow.step("request received", benPage);
      await accept.click();
      await benPage.getByRole("tab", { name: /^Friends/ }).click();
      await expect(
        benPage.getByRole("list", { name: "Your friends" }),
      ).toBeVisible();
      await flow.step("request accepted", benPage);

      await page.reload();
      const friends = page.getByRole("list", { name: "Your friends" });
      await expect(friends).toBeVisible();
      // No shared lesson yet: the friend streak has not started.
      await expect(
        friends.getByText(/Start a friend streak|Friend streak · \d+ day/),
      ).toBeVisible();
      await flow.step("friend streak on the friends list");
    } finally {
      await benContext.close();
    }
  },
);
