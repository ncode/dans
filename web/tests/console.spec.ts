import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, expect, chromium, type Page } from "@playwright/test";
const zone = {
  id: "example.test.",
  name: "example.test.",
  kind: "Native",
  account: "",
  serial: 1,
};
const original = {
  name: "www.example.test.",
  type: "A",
  ttl: 300,
  records: [
    { content: "192.0.2.1", disabled: false },
    { content: "192.0.2.2", disabled: true },
  ],
  comments: [{ content: "Keep comment", account: "test", modified_at: 1 }],
};
async function fixture(page: Page, operator = true) {
  let signedIn = false,
    live = structuredClone(original),
    unknown = false;
  const writes: string[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname.slice("/api/v1".length);
    const reply = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (path === "/dans/session") {
      signedIn = req.method() === "POST";
      return signedIn
        ? reply({ expires_at: "2026-09-20T00:00:00Z" }, 201)
        : route.fulfill({ status: 204 });
    }
    if (!signedIn) return reply({ error: "unauthorized" }, 401);
    if (path === "/dans/me")
      return reply({
        id: "user",
        handle: "test-user",
        display_name: "Test user",
        enabled: true,
        operator,
      });
    if (path === "/dans/me/delegations")
      return reply({
        items: operator
          ? []
          : [
              {
                id: "grant",
                zone_binding_id: "binding",
                zone_id: zone.id,
                zone_name: zone.name,
                grantee_kind: "identity",
                grantee_id: "user",
                selectors: [{ kind: "exact", value: original.name }],
                record_types: ["A"],
                change_kinds: ["REPLACE"],
                created_at: "2026-01-01T00:00:00Z",
                revoked_at: null,
              },
            ],
        next_cursor: null,
      });
    if (path === "/servers/localhost/zones" && req.method() === "GET")
      return reply([zone]);
    if (path.startsWith("/dans/servers/"))
      return reply({
        items: [live],
        next_cursor: null,
        state: "ready",
        last_refreshed_at: "2026-09-12T09:00:00Z",
        error: null,
      });
    if (
      path === "/servers/localhost/zones/example.test." &&
      req.method() === "GET"
    )
      return reply({
        ...zone,
        rrsets: url.searchParams.has("rrset_name") ? [live] : undefined,
      });
    if (req.method() !== "GET") {
      writes.push(req.method() + " " + path);
      if (unknown) return route.abort();
      return route.fulfill({ status: 204 });
    }
    if (path === "/dans/identities")
      return reply({
        items: [
          {
            id: "user",
            handle: "test-user",
            display_name: "Test user",
            enabled: true,
            operator,
          },
        ],
        next_cursor: null,
      });
    if (path === "/dans/zone-bindings")
      return reply({
        items: [
          {
            id: "binding",
            zone_id: zone.id,
            zone_name: zone.name,
            status: "active",
          },
        ],
        next_cursor: null,
      });
    if (path === "/dans/audit-events")
      return reply({
        items: [
          {
            id: "audit",
            event_kind: "dns_outcome",
            action: "powerdns.zone.patch",
            result: "unknown",
            target_type: "zone_binding",
            target_id: "binding",
            occurred_at: "2026-09-12T09:00:00Z",
            request_id: "request",
            details: {},
          },
        ],
        next_cursor: null,
      });
    return reply({ items: [], next_cursor: null });
  });
  return {
    writes,
    change: () => {
      live = { ...live, ttl: 600 };
    },
    unknown: () => {
      unknown = true;
    },
  };
}
async function signIn(page: Page) {
  await page.goto("/console/");
  await page
    .getByLabel("API token", { exact: true })
    .fill("dans_v1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: /^Zones \(1\)$/ }),
  ).toBeVisible();
}
test("login, complete RRset conflict reconciliation and destructive cancellation", async ({
  page,
}) => {
  const state = await fixture(page);
  await signIn(page);
  await page
    .getByRole("link", { name: zone.name, exact: true })
    .first()
    .click();
  await expect(page.getByText("192.0.2.2", { exact: false })).toBeVisible();
  await page.getByRole("button", { name: "Edit www.example.test. A" }).click();
  await expect(page.getByLabel("Record value 2", { exact: true })).toHaveValue(
    "192.0.2.2",
  );
  await expect(page.getByLabel("Comment 1", { exact: true })).toHaveValue(
    "Keep comment",
  );
  await page.getByLabel("TTL (seconds)").fill("400");
  state.change();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(
    page.getByText("This RRset changed while you were editing.", {
      exact: true,
    }),
  ).toBeVisible();
  expect(state.writes).toHaveLength(0);
  await expect(page.getByLabel("TTL (seconds)")).toHaveValue("400");
  await page
    .getByRole("button", { name: "Keep draft and use current as baseline" })
    .click();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("RRset saved.", { exact: true })).toBeVisible();
  expect(state.writes).toHaveLength(1);
  await page
    .getByRole("button", { name: "Delete www.example.test. A" })
    .click();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(state.writes).toHaveLength(1);
});
test("delegated permissions and uncertain write preserve draft", async ({
  page,
}) => {
  const state = await fixture(page, false);
  await signIn(page);
  await expect(
    page.getByRole("link", { name: "Audit", exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("link", { name: zone.name, exact: true })
    .first()
    .click();
  await expect(
    page.getByRole("button", { name: "Delete www.example.test. A" }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Edit www.example.test. A" }).click();
  state.unknown();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText(/Outcome unknown/).first()).toBeVisible();
  await expect(page.getByLabel("Record value 1", { exact: true })).toHaveValue(
    "192.0.2.1",
  );
  expect(state.writes).toHaveLength(1);
});
test("operator administration and narrow signout", async ({ page }) => {
  await fixture(page);
  await signIn(page);
  await page.getByRole("link", { name: "Delegations", exact: true }).click();
  await page
    .getByRole("button", { name: "Create delegation", exact: true })
    .click();
  await expect(page.getByLabel("Name pattern 1")).toBeVisible();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("link", { name: "Audit", exact: true }).click();
  await expect(
    page.getByText("unknown", { exact: true }).first(),
  ).toBeVisible();
  await page.setViewportSize({ width: 320, height: 740 });
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByLabel("API token", { exact: true })).toBeVisible();
  expect(
    await page.evaluate(() => ({
      local: localStorage.length,
      session: sessionStorage.length,
    })),
  ).toEqual({ local: 0, session: 0 });
});

test("failed signout clears protected content and preserves a recoverable error", async ({
  page,
}) => {
  await fixture(page);
  await signIn(page);
  await page.route("**/api/v1/dans/session", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "unavailable" }),
    }),
  );
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByLabel("API token", { exact: true })).toBeVisible();
  await expect(page.getByText(/Sign-out could not be confirmed/)).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Zones", exact: true }),
  ).toHaveCount(0);
});

test("index loading, server pages, filters, stale results and empty results", async ({
  page,
}) => {
  await fixture(page);
  let mode = "indexing";
  const queries: URLSearchParams[] = [];
  await page.route("**/api/v1/dans/servers/**", async (route) => {
    const query = new URL(route.request().url()).searchParams;
    queries.push(query);
    if (route.request().method() === "POST") mode = "stale";
    const second = query.get("cursor") === "next-page";
    const items =
      mode === "indexing" || mode === "empty"
        ? []
        : [{ ...original, name: second ? "zzz.example.test." : original.name }];
    await route.fulfill({
      status: mode === "indexing" ? 202 : 200,
      contentType: "application/json",
      body: JSON.stringify({
        items,
        next_cursor: mode === "ready" && !second ? "next-page" : null,
        state: mode === "empty" ? "ready" : mode,
        last_refreshed_at: mode === "indexing" ? null : "2026-09-12T09:00:00Z",
        error: mode === "stale" ? "Refresh failed." : null,
      }),
    });
  });
  await signIn(page);
  await page
    .getByRole("link", { name: zone.name, exact: true })
    .first()
    .click();
  await expect(page.getByText("Indexing zone", { exact: true })).toBeVisible();
  mode = "ready";
  await expect(
    page.getByRole("button", { name: "Next", exact: true }),
  ).toBeEnabled({ timeout: 8000 });
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByText("zzz.example.test.", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Previous", exact: true }).click();
  await expect(page.getByText(original.name, { exact: true })).toBeVisible();
  await page.getByLabel("Owner name", { exact: true }).fill("www.");
  await page.getByLabel("Record type", { exact: true }).fill("A");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect
    .poll(() =>
      queries.some(
        (query) =>
          query.get("name") === "www." &&
          query.get("type") === "A" &&
          !query.has("cursor"),
      ),
    )
    .toBe(true);
  await page.getByRole("button", { name: "Refresh zone", exact: true }).click();
  await expect(
    page.getByText("Stale browsing data", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText(original.name, { exact: true })).toBeVisible();
  mode = "empty";
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(
    page.getByText("No RRsets match the selected filters.", { exact: true }),
  ).toBeVisible();
});

test("creation collisions and mid-edit denial preserve the draft", async ({
  page,
}) => {
  const state = await fixture(page);
  await signIn(page);
  await page
    .getByRole("link", { name: zone.name, exact: true })
    .first()
    .click();
  await page.getByRole("button", { name: "Create RRset", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Owner name", { exact: true }).fill(original.name);
  await dialog.getByLabel("Record value 1", { exact: true }).fill("192.0.2.9");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(
    page.getByText("This RRset changed while you were editing.", {
      exact: true,
    }),
  ).toBeVisible();
  expect(state.writes).toHaveLength(0);
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("button", { name: "Edit www.example.test. A" }).click();
  await page.route(
    "**/api/v1/servers/localhost/zones/example.test.",
    (route) =>
      route.request().method() === "PATCH"
        ? route.fulfill({
            status: 403,
            contentType: "application/json",
            body: JSON.stringify({
              error: "Permission changed. Reload your permissions.",
            }),
          })
        : route.fallback(),
  );
  await dialog.getByLabel("TTL (seconds)").fill("401");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(
    page.getByText("Permission changed. Reload your permissions.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(dialog.getByLabel("TTL (seconds)")).toHaveValue("401");
});

test("keyboard focus, narrow layout and complete long values", async ({
  page,
}) => {
  await fixture(page);
  await page.route("**/api/v1/dans/servers/**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [
          {
            ...original,
            name: "*.example.test.",
            type: "TXT",
            records: [
              {
                content: '"' + "long-value ".repeat(60) + '"',
                disabled: false,
              },
            ],
          },
        ],
        next_cursor: null,
        state: "ready",
        last_refreshed_at: "2026-09-12T09:00:00Z",
        error: null,
      }),
    }),
  );
  await signIn(page);
  const zoneLink = page
    .getByRole("link", { name: zone.name, exact: true })
    .first();
  await zoneLink.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: zone.name, exact: true }),
  ).toBeFocused();
  await page
    .getByRole("link", { name: "Skip to content", exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#main-content")).toBeFocused();
  await expect(
    page.getByRole("heading", { name: zone.name, exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Create RRset", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Create RRset", exact: true }),
  ).toBeFocused();
  await page.setViewportSize({ width: 320, height: 740 });
  await expect(page.getByLabel("Owner name", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Create RRset", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBe(true);
  await page.screenshot({
    path: process.env.CONSOLE_SCREENSHOT_DIR
      ? process.env.CONSOLE_SCREENSHOT_DIR + "/narrow.png"
      : test.info().outputPath("narrow.png"),
    fullPage: true,
  });
});

test("native 200 percent browser zoom keeps forms usable", async () => {
  const profile = await mkdtemp(join(tmpdir(), "console-zoom-"));
  try {
    await mkdir(join(profile, "Default"));
    // Chromium stores native page zoom as log(zoomFactor)/log(1.2), by storage partition.
    await writeFile(
      join(profile, "Default", "Preferences"),
      JSON.stringify({
        partition: { default_zoom_level: { x: Math.log(2) / Math.log(1.2) } },
      }),
    );
    const context = await chromium.launchPersistentContext(profile, {
      channel: "chromium",
      headless: true,
      viewport: null,
      baseURL: "http://localhost:5173",
      args: ["--window-size=1280,960"],
    });
    try {
      const page = context.pages()[0];
      await fixture(page);
      await signIn(page);
      expect(
        await page.evaluate(() => ({
          width: innerWidth,
          scale: devicePixelRatio,
        })),
      ).toEqual({ width: 640, scale: 2 });
      await page
        .getByRole("link", { name: zone.name, exact: true })
        .first()
        .click();
      await expect(
        page.getByLabel("Owner name", { exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: "Search", exact: true }),
      ).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBe(true);
      const cdp = await context.newCDPSession(page);
      const capture = await cdp.send("Page.captureScreenshot", {
        format: "png",
        captureBeyondViewport: false,
      });
      await writeFile(
        test.info().outputPath("native-zoom.png"),
        Buffer.from(capture.data, "base64"),
      );
      await cdp.detach();
      await page
        .getByRole("button", { name: "Create RRset", exact: true })
        .click();
      await expect(
        page.getByRole("dialog").getByLabel("Owner name", { exact: true }),
      ).toBeVisible();
      await expect(
        page
          .getByRole("dialog")
          .getByRole("button", { name: "Save", exact: true }),
      ).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(
        page.getByRole("button", { name: "Create RRset", exact: true }),
      ).toBeFocused();
    } finally {
      await context.close();
    }
  } finally {
    await rm(profile, { recursive: true, force: true });
  }
});

test("failed browse requests retain rows with an explicit stale state", async ({
  page,
}) => {
  await fixture(page);
  await signIn(page);
  await page
    .getByRole("link", { name: zone.name, exact: true })
    .first()
    .click();
  await expect(page.getByText("Up to date", { exact: true })).toBeVisible();
  await page.route("**/api/v1/dans/servers/**", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "Browsing is temporarily unavailable." }),
    }),
  );
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(
    page.getByText("Browsing is temporarily unavailable.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Stale browsing data", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText(original.name, { exact: true })).toBeVisible();
});
