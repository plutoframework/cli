// Browser QA for disposable consumers from frontend-matrix.py --keep.
// Pass an installed @playwright/test/index.mjs path as the second argument.
import { pathToFileURL } from "node:url";
import { spawn, execFileSync } from "node:child_process";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

const root = path.resolve(process.argv[2]);
const { chromium, expect } = await import(
  pathToFileURL(path.resolve(process.argv[3]))
);
const browser = await chromium.launch({ channel: "chrome" });
const evidence = path.join(root, "screenshots");
await mkdir(evidence, { recursive: true });

try {
  for (const [index, profile] of ["native", "heroui-v3", "shadcn"].entries()) {
    const cwd = path.join(root, profile, "web");
    execFileSync("node", ["scripts/certificate.mjs"], { cwd, stdio: "ignore" });
    const port = 5181 + index;
    const server = spawn(
      process.execPath,
      ["node_modules/vite/bin/vite.js", "preview", "--port", String(port)],
      { cwd, stdio: "ignore" },
    );
    const context = await browser.newContext({
      // Only the fixed numeric-loopback consumer servers use self-signed TLS.
      ignoreHTTPSErrors: true,
      viewport: { width: 1440, height: 1000 },
    });
    try {
      let signedIn = false;
      const page = await context.newPage();
      const errors = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.route("**/auth/**", async (route) => {
        const request = route.request();
        const pathname = new URL(request.url()).pathname;
        if (request.method() === "GET") {
          await route.fulfill({
            status: signedIn ? 200 : 401,
            json: signedIn
              ? {
                  data: {
                    id: "synthetic-session",
                    actor_id: "synthetic-account",
                    expires_at: "2030-01-01T00:00:00Z",
                  },
                }
              : { code: "unauthenticated" },
          });
          return;
        }
        assert.equal(request.headers()["origin"], `https://127.0.0.1:${port}`);
        assert.equal(request.headers()["idempotency-key"], undefined);
        assert.equal(request.headers()["authorization"], undefined);
        if (pathname === "/auth/signin") signedIn = true;
        if (pathname === "/auth/logout" || pathname === "/auth/reset")
          signedIn = false;
        await route.fulfill({
          status: [
            "/auth/enroll",
            "/auth/recovery",
            "/auth/verification",
          ].includes(pathname)
            ? 202
            : 200,
          json: { code: "ok" },
        });
      });
      const url = `https://127.0.0.1:${port}`;
      // A bounded startup retry handles only the consumer server we just spawned.
      await expect(async () => {
        await page.goto(url, { timeout: 3000 });
        await expect(
          page.getByRole("heading", { name: /A little structure/ }),
        ).toBeVisible();
      }).toPass({ timeout: 15000 });
      await page.screenshot({
        path: path.join(evidence, `${profile}-overview.png`),
        fullPage: true,
      });
      await page.getByRole("link", { name: "Components", exact: true }).click();
      const form = page
        .getByRole("article")
        .filter({
          has: page.getByRole("heading", { name: "sample-form", exact: true }),
        });
      await form
        .getByLabel("Title", { exact: true })
        .fill("Original synthetic request");
      await form.getByRole("button", { name: "Continue", exact: true }).click();
      await expect(form.getByRole("status")).toContainText(
        "No data has been saved",
      );
      await page.getByRole("button", { name: "Open details" }).click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(page.getByRole("dialog")).toBeHidden();
      await expect(
        page.getByRole("button", { name: "Open details" }),
      ).toBeFocused();
      await page.screenshot({
        path: path.join(evidence, `${profile}-components.png`),
        fullPage: true,
      });
      await page.getByRole("link", { name: "Account", exact: true }).click();
      await expect(
        page.getByRole("heading", { name: "Sign in", exact: true }),
      ).toBeVisible();
      await page
        .getByLabel("Email address", { exact: true })
        .fill("synthetic@example.test");
      await page
        .getByLabel("Password", { exact: true })
        .fill("original-synthetic-password");
      await page.getByRole("button", { name: "Sign in", exact: true }).click();
      await expect(
        page.getByRole("heading", { name: "Welcome back." }),
      ).toBeVisible();
      await page.getByRole("button", { name: "Sign out", exact: true }).click();
      await expect(
        page.getByRole("heading", { name: "Sign in", exact: true }),
      ).toBeVisible();
      await page.screenshot({
        path: path.join(evidence, `${profile}-auth.png`),
        fullPage: true,
      });
      for (const width of [375, 320]) {
        await page.setViewportSize({ width, height: 812 });
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
          true,
        );
        await page.screenshot({
          path: path.join(evidence, `${profile}-auth-${width}.png`),
          fullPage: true,
        });
      }
      assert.equal(
        await page.evaluate(() => localStorage.length + sessionStorage.length),
        0,
      );
      assert.deepEqual(errors, []);
      console.log(
        `${profile}: forms, dialog focus, auth contract, storage and mobile bounds passed`,
      );
    } finally {
      await context.close();
      server.kill("SIGTERM");
      await new Promise((resolve) => server.once("exit", resolve));
    }
  }
} finally {
  await browser.close();
}
console.log(`Screenshots: ${evidence}`);
