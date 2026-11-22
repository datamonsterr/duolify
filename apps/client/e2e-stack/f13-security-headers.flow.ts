import { expect, stackURL, test } from "./stack";

test.use({ flowId: "F13" });

// Headers as the browser receives them from the proxied stack (Caddy in front of the web client and the API).
test(
  "F13 the web client, the lesson frame and the API send the security headers",
  {
    annotation: {
      type: "flow",
      description: "F13 Security headers on the proxied stack",
    },
  },
  async ({ page, request, flow }) => {
    const hsts = "max-age=31536000; includeSubDomains";
    const web = await page.goto("/sign-in");
    const headers = web!.headers();
    expect(headers["strict-transport-security"]).toBe(hsts);
    expect(headers["x-content-type-options"]).toBe("nosniff");
    expect(headers["referrer-policy"]).toBe("strict-origin-when-cross-origin");
    expect(headers["permissions-policy"]).toContain("microphone=(self)");
    const csp = headers["content-security-policy"]!;
    for (const part of [
      "default-src 'self'",
      "frame-ancestors 'none'",
      "object-src 'none'",
      "base-uri 'self'",
    ])
      expect(csp).toContain(part);
    expect(csp).not.toContain("unsafe-eval");
    await expect(
      page.getByRole("button", { name: "Get started" }),
    ).toBeVisible();
    await flow.step("sign-in page under the CSP");

    const frame = await request.get(`${stackURL}/lesson-frame.html`);
    expect(frame.headers()["content-security-policy"]).toContain(
      "frame-ancestors 'self'",
    );

    const api = await request.get(`${stackURL}/v1/features`);
    expect(api.headers()["strict-transport-security"]).toBe(hsts);
    expect(api.headers()["content-security-policy"]).toBe(
      "default-src 'none'; frame-ancestors 'none'",
    );
    expect(api.headers()["referrer-policy"]).toBe("no-referrer");
    expect(api.headers()["x-content-type-options"]).toBe("nosniff");
  },
);
