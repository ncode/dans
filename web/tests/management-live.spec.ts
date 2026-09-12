import { readFileSync, writeFileSync } from "node:fs";
import { test, expect } from "@playwright/test";

const origin = process.env.CONSOLE_LIVE_URL;
const tokenFile = process.env.CONSOLE_LIVE_TOKEN_FILE;
test("real management workflows and complete capacity download", async ({
  page,
  browserName,
}) => {
  test.skip(
    !origin || !tokenFile,
    "Requires an isolated, explicitly seeded live management fixture",
  );
  test.setTimeout(180000);
  const collectionResponses: { rows: number; bytes: number }[] = [];
  const pending: Promise<void>[] = [];
  page.on("response", (response) => {
    if (
      !response.url().includes("/api/v1/dans/") ||
      response.url().includes("/export")
    )
      return;
    pending.push(
      response
        .body()
        .then((body) => {
          const json = JSON.parse(body.toString());
          if (Array.isArray(json.items))
            collectionResponses.push({
              rows: json.items.length,
              bytes: body.length,
            });
        })
        .catch(() => {
          /* Redirect and cancelled responses have no readable body. */
        }),
    );
  });
  await page.goto(origin + "/console/");
  await page
    .getByLabel("API token", { exact: true })
    .fill(readFileSync(tokenFile!, "utf8").trim());
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: /^Zones \(/ })).toBeVisible();
  const cookie = (await page.context().cookies()).find(
    (c) => c.name === "dans_dev_session",
  );
  expect(cookie?.httpOnly).toBe(true);
  expect(cookie?.sameSite).toBe("Strict");
  await page.reload();
  await expect(page.getByRole("heading", { name: /^Zones \(/ })).toBeVisible();

  await page.getByRole("link", { name: "Identities", exact: true }).click();
  await expect(page.getByRole("row")).toHaveCount(101);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Previous", exact: true }),
  ).toBeEnabled();
  await page
    .getByLabel("Handle prefix", { exact: true })
    .fill("capacity-identity-0001");
  await page
    .getByRole("button", { name: "Search identities", exact: true })
    .click();
  await page
    .getByRole("link", { name: "capacity-identity-0001", exact: true })
    .click();
  await page.getByRole("tab", { name: "API tokens", exact: true }).click();
  await expect(page.getByRole("row")).toHaveCount(101);
  await page.getByRole("tab", { name: "Groups", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "capacity-group-001", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("tab", { name: "Effective authority now", exact: true })
    .click();
  await expect(page.getByText(/No effective delegations/)).toBeVisible();
  await page
    .getByRole("tab", { name: "Retained assignments", exact: true })
    .click();
  await expect(
    page.getByText("No retained assignments.", { exact: true }),
  ).toBeVisible();

  await page.goto(
    origin + "/console/#/groups/20000001-0000-4000-8000-000000000000",
  );
  await expect(page.getByRole("row")).toHaveCount(101);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Previous", exact: true }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "Add member", exact: true }).click();
  let dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Identity handle prefix", { exact: true })
    .fill("capacity-identity-0001");
  await dialog
    .getByRole("button", { name: "Search identities", exact: true })
    .click();
  await dialog.getByLabel("Identity", { exact: true }).click();
  await page.getByRole("option", { name: /capacity-identity-0001/ }).click();
  await dialog.getByRole("button", { name: "Add member", exact: true }).click();
  await expect(dialog).toHaveCount(0);

  // Create through the UI, then issue a credential as a separate explicit action.
  const handle = "browser-" + browserName + "-" + Date.now();
  await page.getByRole("link", { name: "Identities", exact: true }).click();
  await page
    .getByRole("button", { name: "Create identity", exact: true })
    .click();
  await dialog.getByLabel("Handle", { exact: true }).fill(handle);
  await dialog
    .getByLabel("Display name", { exact: true })
    .fill("Synthetic browser identity");
  await dialog
    .getByRole("button", { name: "Create identity", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: handle, exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Edit profile", exact: true }).click();
  await dialog
    .getByLabel("Display name", { exact: true })
    .fill("Updated synthetic identity");
  await dialog
    .getByRole("button", { name: "Save profile", exact: true })
    .click();
  await expect(
    page.getByText("Updated synthetic identity", { exact: true }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "API tokens", exact: true }).click();
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  await dialog.getByLabel("Label", { exact: true }).fill("browser-test");
  await dialog
    .getByRole("button", { name: "Create token", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Copy your new API token", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Copy token", exact: true }).click();
  await expect(
    page.getByText(/Token copied.|Clipboard access was unavailable/),
  ).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await page
    .getByRole("button", { name: "Revoke browser-test", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Revoke token", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Revoke browser-test", exact: true }),
  ).toBeDisabled();
  await page.getByRole("tab", { name: "Profile", exact: true }).click();
  await page
    .getByRole("button", { name: "Disable identity", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Disable identity", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Enable identity", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Enable identity", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Enable identity", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Disable identity", exact: true }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Audit", exact: true }).click();
  await page.getByLabel("Action", { exact: true }).fill("synthetic.capacity");
  await page.getByRole("button", { name: "Filter audit", exact: true }).click();
  await expect(page.getByRole("row")).toHaveCount(101);
  const started = performance.now();
  let rows = 0,
    bytes = 0;
  await test.step("complete native streamed download", async () => {
    console.log("Starting native capacity download");
    const startedDownload = page.waitForEvent("download");
    await page
      .getByRole("link", { name: "Download NDJSON", exact: true })
      .click();
    const download = await startedDownload;
    console.log("Native download started");
    expect(await download.failure()).toBeNull();
    console.log("Native download completed");
    const stream = await download.createReadStream();
    let incomplete = "";
    for await (const chunk of stream) {
      bytes += chunk.length;
      const lines = (incomplete + chunk.toString()).split("\n");
      incomplete = lines.pop() || "";
      for (const line of lines) {
        const event = JSON.parse(line);
        if (event.action !== "synthetic.capacity")
          throw new Error(
            "Export contained an event outside the selected filter",
          );
        rows++;
      }
    }
    expect(incomplete).toBe("");
    expect(rows).toBe(100000);
  });
  await Promise.all(pending);
  expect(collectionResponses.every((item) => item.rows <= 100)).toBe(true);
  const evidence = {
    browser: browserName,
    audit_rows: rows,
    audit_bytes: bytes,
    download_and_validation_ms: Math.round(performance.now() - started),
    maximum_collection_rows: Math.max(
      ...collectionResponses.map((item) => item.rows),
    ),
    maximum_collection_bytes: Math.max(
      ...collectionResponses.map((item) => item.bytes),
    ),
  };
  writeFileSync(
    test.info().outputPath("capacity.json"),
    JSON.stringify(evidence, null, 2),
  );
  await page.screenshot({
    path: test.info().outputPath("management-live.png"),
    fullPage: true,
  });
  await page.getByRole("link", { name: "My access", exact: true }).click();
  await page.getByRole("tab", { name: "API tokens", exact: true }).click();
  await expect(
    page.getByText("Current sign-in", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByLabel("API token", { exact: true })).toBeVisible();
});
