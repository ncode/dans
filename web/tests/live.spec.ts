import { readFileSync } from "node:fs";
import { test, expect } from "@playwright/test";
const origin = process.env.CONSOLE_LIVE_URL;
const tokenFile = process.env.CONSOLE_LIVE_TOKEN_FILE;
const secondary = process.env.CONSOLE_LIVE_SECONDARY_URL;
const zone = "console-capacity-001.test.";

test("embedded console against real API and authoritative DNS", async ({
  page,
}) => {
  test.skip(
    !origin || !tokenFile,
    "Requires an explicitly configured isolated live fixture",
  );
  test.setTimeout(120000);
  const requested: string[] = [];
  page.on("request", (req) => requested.push(req.url()));
  await page.goto(origin + "/console/");
  await expect(page.getByLabel("API token", { exact: true })).toBeVisible();
  await page
    .getByLabel("API token", { exact: true })
    .fill(readFileSync(tokenFile!, "utf8").trim());
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: /^Zones \(/ })).toBeVisible();
  const cookies = await page.context().cookies();
  expect(
    cookies.some(
      (cookie) =>
        cookie.name === "__Host-dans_session" &&
        cookie.httpOnly &&
        cookie.secure &&
        cookie.sameSite === "Strict",
    ),
  ).toBe(true);
  await page.reload();
  await expect(page.getByRole("heading", { name: /^Zones \(/ })).toBeVisible();
  if (secondary) {
    await page.goto(secondary + "/console/");
    await expect(
      page.getByRole("heading", { name: /^Zones \(/ }),
    ).toBeVisible();
    await page.goto(origin + "/console/");
  }
  await page.getByRole("link", { name: zone, exact: true }).click();
  await page.getByRole("button", { name: "Create RRset", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const owner = "ui-check-" + Date.now() + "." + zone;
  await dialog.getByLabel("Owner name", { exact: true }).fill(owner);
  await dialog.getByLabel("Record value 1", { exact: true }).fill("192.0.2.80");
  await dialog.getByRole("button", { name: "Add value", exact: true }).click();
  await dialog.getByLabel("Record value 2", { exact: true }).fill("192.0.2.81");
  await dialog
    .getByRole("checkbox", { name: "Disabled", exact: true })
    .nth(1)
    .check();
  await dialog
    .getByRole("button", { name: "Add comment", exact: true })
    .click();
  await dialog
    .getByLabel("Comment 1", { exact: true })
    .fill("Synthetic browser verification");
  await dialog.getByLabel("Comment author 1", { exact: true }).fill("test");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("RRset saved.", { exact: true })).toBeVisible();
  await page.getByLabel("Owner name", { exact: true }).fill(owner);
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Edit " + owner + " A", exact: true }),
  ).toBeVisible({ timeout: 15000 });
  await page
    .getByRole("button", { name: "Edit " + owner + " A", exact: true })
    .click();
  await expect(
    dialog.getByLabel("Record value 2", { exact: true }),
  ).toHaveValue("192.0.2.81");
  await expect(
    dialog.getByRole("checkbox", { name: "Disabled", exact: true }).nth(1),
  ).toBeChecked();
  await expect(dialog.getByLabel("Comment 1", { exact: true })).toHaveValue(
    "Synthetic browser verification",
  );
  await dialog.getByLabel("TTL (seconds)").fill("600");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.screenshot({
    path: test.info().outputPath("live-console.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Delete " + owner + " A", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Delete RRset", exact: true })
    .click();
  await expect(page.getByText("RRset deleted.", { exact: true })).toBeVisible({
    timeout: 35000,
  });

  const identityStatus = await page.evaluate(
    async () =>
      (
        await fetch("/api/v1/dans/identities", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            handle: "console-browser-user",
            kind: "user",
            display_name: "Browser test user",
          }),
        })
      ).status,
  );
  expect([201, 409]).toContain(identityStatus);
  await page.getByRole("link", { name: "Delegations", exact: true }).click();
  await page
    .getByRole("button", { name: "Create delegation", exact: true })
    .click();
  await dialog.getByLabel("Zone", { exact: true }).click();
  await page.getByRole("option", { name: zone, exact: true }).click();
  await dialog.getByLabel("Grantee", { exact: true }).click();
  await page
    .getByRole("option", {
      name: "Browser test user (console-browser-user)",
      exact: true,
    })
    .click();
  await dialog.getByLabel("Name pattern 1").fill(owner);
  await dialog
    .getByRole("button", { name: "Create delegation", exact: true })
    .click();
  await expect(
    page.getByText("Delegation created.", { exact: true }),
  ).toBeVisible();
  const row = page.getByRole("row").filter({ hasText: owner }).first();
  await row.getByRole("button", { name: "Inspect", exact: true }).click();
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await row.getByRole("button", { name: "Revoke", exact: true }).click();
  await dialog
    .getByRole("button", { name: "Revoke delegation", exact: true })
    .click();
  await expect(
    page.getByText("Delegation revoked.", { exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Audit", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Audit", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("succeeded", { exact: true }).first(),
  ).toBeVisible();
  await page
    .getByRole("button", { name: /^Inspect audit / })
    .first()
    .click();
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("link", { name: "Zones", exact: true }).first().click();
  await page.getByRole("button", { name: "Create zone", exact: true }).click();
  const temporaryZone = "ui-zone-" + Date.now() + ".test.";
  await dialog.getByLabel("Zone name", { exact: true }).fill(temporaryZone);
  await dialog
    .getByLabel("Nameservers", { exact: true })
    .fill("ns1.example.test., ns2.example.test.");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Zone created.", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: temporaryZone, exact: true }).click();
  await page.getByRole("button", { name: "Edit zone", exact: true }).click();
  await dialog
    .getByLabel("Account label", { exact: true })
    .fill("Synthetic browser account");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Zone saved.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Edit zone", exact: true }).click();
  await expect(dialog.getByLabel("Account label", { exact: true })).toHaveValue(
    "Synthetic browser account",
  );
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("button", { name: "Delete zone", exact: true }).click();
  await dialog
    .getByRole("button", { name: "Delete zone", exact: true })
    .click();
  await expect(page.getByText("Zone deleted.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByLabel("API token", { exact: true })).toBeVisible();
  expect(
    (await page.context().cookies()).some(
      (cookie) => cookie.name === "__Host-dans_session",
    ),
  ).toBe(false);
  expect(
    requested.every((url) =>
      [origin, secondary]
        .filter(Boolean)
        .some((value) => new URL(url).origin === new URL(value!).origin),
    ),
  ).toBe(true);
});
