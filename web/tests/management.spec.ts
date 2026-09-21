import { test, expect, type Page } from "@playwright/test";
import { createServer } from "node:http";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "@playwright/test";

const actor = {
  id: "actor",
  kind: "user",
  handle: "operator",
  display_name: "Test operator",
  enabled: true,
  operator: true,
};
const member = {
  ...actor,
  id: "member",
  handle: "team_member",
  display_name: "Team member",
  operator: false,
  enabled: false,
};
const group = {
  id: "group",
  handle: "team_group",
  display_name: "Team group",
  enabled: false,
};
export async function managementFixture(page: Page) {
  let current = { ...actor },
    target = { ...member },
    currentGroup = { ...group },
    signedIn = true;
  const requests: {
    path: string;
    method: string;
    query: URLSearchParams;
    body: any;
  }[] = [];
  const tokens = [
    {
      id: "sign-in-token",
      identity_id: "actor",
      label: "browser",
      created_at: "2026-01-01T00:00:00Z",
      expires_at: null,
      revoked_at: null,
    },
  ];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname.slice(7),
      method = req.method();
    requests.push({
      path,
      method,
      query: url.searchParams,
      body: req.postDataJSON(),
    });
    const reply = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    const list = (items: unknown[], next_cursor: string | null = null) =>
      reply({ items, next_cursor });
    if (path === "/dans/session") {
      signedIn = method === "POST";
      return route.fulfill({ status: 204 });
    }
    if (!signedIn) return reply({ error: "unauthorized" }, 401);
    if (path === "/dans/me") return reply(current);
    if (path === "/dans/me/credential")
      return reply({ token_id: "sign-in-token" });
    if (path === "/servers/localhost/zones") return reply([]);
    if (path === "/dans/identities") {
      if (method === "POST")
        return reply({ ...target, ...req.postDataJSON(), enabled: true }, 201);
      return list(
        url.searchParams.has("handle_prefix") || url.searchParams.has("cursor")
          ? [target]
          : [current],
        url.searchParams.has("handle_prefix") || url.searchParams.has("cursor")
          ? null
          : "identity-page-2",
      );
    }
    if (path === "/dans/identities/member") {
      if (method === "PATCH") target = { ...target, ...req.postDataJSON() };
      return reply(target);
    }
    if (path === "/dans/identities/actor") {
      if (method === "PATCH") {
        current = { ...current, ...req.postDataJSON() };
        if (!current.enabled) signedIn = false;
      }
      return reply(current);
    }
    if (path === "/dans/groups") {
      if (method === "POST") {
        currentGroup = {
          ...currentGroup,
          ...req.postDataJSON(),
          enabled: true,
        };
        return reply(currentGroup, 201);
      }
      return list([currentGroup]);
    }
    if (path === "/dans/groups/group") {
      if (method === "PATCH")
        currentGroup = { ...currentGroup, ...req.postDataJSON() };
      return reply(currentGroup);
    }
    if (path.endsWith("/members"))
      return list(
        [target],
        url.searchParams.has("cursor") ? null : "members-page-2",
      );
    if (path.includes("/members/")) return route.fulfill({ status: 204 });
    if (path.endsWith("/groups")) return list([currentGroup]);
    if (path.endsWith("/delegations") || path.endsWith("/assignments"))
      return list([]);
    if (path.endsWith("/tokens")) {
      if (method === "POST")
        return reply(
          {
            ...tokens[0],
            id: "new-token",
            label: req.postDataJSON().label,
            secret: "dans_v1_once_only_synthetic_secret",
          },
          201,
        );
      return list(tokens);
    }
    if (path.includes("/tokens/")) {
      if (path.endsWith("sign-in-token")) signedIn = false;
      return route.fulfill({ status: 204 });
    }
    if (path === "/dans/zone-bindings")
      return list([
        {
          id: "binding",
          zone_id: "example.test.",
          zone_name: "example.test.",
          generation: 1,
          status: "retired",
          deletion_state: "unknown",
          recovery_actions: [
            "observe",
            "retry_delete",
            "confirm_absent",
            "rebind",
          ],
        },
      ]);
    if (path === "/dans/zone-bindings/binding")
      return reply({
        id: "binding",
        zone_id: "example.test.",
        zone_name: "example.test.",
        generation: 1,
        status: "retired",
        deletion_state: "unknown",
        recovery_actions: [
          "observe",
          "retry_delete",
          "confirm_absent",
          "rebind",
        ],
      });
    if (path.endsWith("/observe"))
      return reply({
        binding_id: "binding",
        zone_present: true,
        observed_zone_id: "example.test.",
        observed_at: "2026-01-01T00:00:00Z",
      });
    if (path.includes("/zone-bindings/binding/"))
      return route.fulfill({ status: 204 });
    if (path === "/dans/audit-events") return list([]);
    return reply({ error: "unexpected fixture request " + path }, 404);
  });
  return { requests };
}

test("identity search, details and explicit restoration confirmation", async ({
  page,
}) => {
  const state = await managementFixture(page);
  await page.goto("/console/#/identities");
  await expect(
    page.getByRole("heading", { name: "Identities", exact: true }),
  ).toBeVisible();
  expect(
    state.requests.filter((r) => r.path === "/dans/identities"),
  ).toHaveLength(1);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await page.getByRole("link", { name: "team_member", exact: true }).click();
  await expect(
    page.getByText("Effective authority now", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(/Disabled identities have no usable authority/),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Enable identity", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toContainText("restore");
  expect(state.requests.filter((r) => r.method === "PATCH")).toHaveLength(0);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Enable identity", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Disable identity", exact: true }),
  ).toBeVisible();
});

test("self-service token is one-time and sign-in token revocation ends access", async ({
  page,
}) => {
  await managementFixture(page);
  await page.goto("/console/#/me");
  await page.getByRole("tab", { name: "API tokens", exact: true }).click();
  await expect(
    page.getByText("Current sign-in", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByLabel("Label", { exact: true })
    .fill("automation");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Create token", exact: true })
    .click();
  await expect(
    page.getByText("dans_v1_once_only_synthetic_secret", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Copy token", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expect(
    page.getByText("dans_v1_once_only_synthetic_secret", { exact: true }),
  ).toHaveCount(0);
  expect(
    await page.evaluate(() => [localStorage.length, sessionStorage.length]),
  ).toEqual([0, 0]);
  await page
    .getByRole("button", { name: "Revoke browser", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toContainText("sign-in");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Revoke token", exact: true })
    .click();
  await expect(page.getByLabel("API token", { exact: true })).toBeVisible();
});

test("group members and bounded remote membership selection", async ({
  page,
}) => {
  const state = await managementFixture(page);
  await page.goto("/console/#/groups/group");
  await page.getByRole("button", { name: "Add member", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByLabel("Identity handle prefix", { exact: true })
    .fill("team_");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Search identities", exact: true })
    .click();
  await expect
    .poll(() =>
      state.requests.some(
        (r) =>
          r.path === "/dans/identities" &&
          r.query.get("handle_prefix") === "team_",
      ),
    )
    .toBe(true);
  await page
    .getByRole("dialog")
    .getByLabel("Identity", { exact: true })
    .click();
  await page.getByRole("option", { name: /team_member/ }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Add member", exact: true })
    .click();
  await expect
    .poll(() =>
      state.requests.some(
        (r) => r.method === "PUT" && r.path.endsWith("/members/member"),
      ),
    )
    .toBe(true);
});

test("binding observation is separate from explicitly confirmed retry", async ({
  page,
}) => {
  const state = await managementFixture(page);
  await page.goto("/console/#/bindings/binding");
  await page
    .getByRole("button", { name: "Observe upstream", exact: true })
    .click();
  await expect(page.getByText(/Zone is present upstream/)).toBeVisible();
  expect(
    state.requests.filter((r) => r.path.endsWith("/retry-delete")),
  ).toHaveLength(0);
  await page
    .getByRole("button", { name: "Retry deletion", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toContainText("delegations");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Retry deletion", exact: true })
    .click();
  await expect
    .poll(
      () =>
        state.requests.filter((r) => r.path.endsWith("/retry-delete")).length,
    )
    .toBe(1);
});

test("identity creation is separate from tokens and service kind is explicit", async ({
  page,
}) => {
  const state = await managementFixture(page);
  await page.goto("/console/#/identities");
  await page
    .getByRole("button", { name: "Create identity", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Handle", { exact: true }).fill("automation-agent");
  await dialog
    .getByLabel("Display name", { exact: true })
    .fill("Automation agent");
  await dialog.getByLabel("Identity kind", { exact: true }).click();
  await page
    .getByRole("option", { name: "Service identity", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Create identity", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  expect(
    state.requests.filter(
      (r) => r.method === "POST" && r.path.endsWith("/tokens"),
    ),
  ).toHaveLength(0);
  expect(
    state.requests.find(
      (r) => r.method === "POST" && r.path === "/dans/identities",
    )?.body,
  ).toMatchObject({ handle: "automation-agent", kind: "service" });
});

test("self-demotion updates navigation and last-operator rejection preserves access", async ({
  page,
}) => {
  const state = await managementFixture(page);
  await page.goto("/console/#/identities/actor");
  await page
    .getByRole("button", { name: "Remove DANS operator role", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("last enabled DANS operator");
  await page.route("**/dans/identities/actor", (route) =>
    route.request().method() === "PATCH"
      ? route.fulfill({
          status: 409,
          contentType: "application/json",
          body: JSON.stringify({
            error: "The last enabled DANS operator must remain.",
          }),
        })
      : route.fallback(),
  );
  await dialog
    .getByRole("button", { name: "Remove DANS operator role", exact: true })
    .click();
  await expect(dialog).toContainText("must remain");
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.unroute("**/dans/identities/actor");
  await page
    .getByRole("button", { name: "Remove DANS operator role", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Remove DANS operator role", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "My access", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Identities", exact: true }),
  ).toHaveCount(0);
  expect(state.requests.filter((r) => r.method === "PATCH")).toHaveLength(1);
});

test("unknown token issuance keeps inputs and never automatically submits again", async ({
  page,
}) => {
  await managementFixture(page);
  let attempts = 0;
  await page.route("**/dans/me/tokens", (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    attempts++;
    return route.abort();
  });
  await page.goto("/console/#/me");
  await page.getByRole("tab", { name: "API tokens", exact: true }).click();
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Label", { exact: true }).fill("uncertain-token");
  await dialog
    .getByRole("button", { name: "Create token", exact: true })
    .click();
  await expect(dialog).toContainText("Outcome unknown");
  await expect(dialog.getByLabel("Label", { exact: true })).toHaveValue(
    "uncertain-token",
  );
  await expect(
    dialog.getByRole("button", { name: "Create token", exact: true }),
  ).toBeEnabled();
  expect(attempts).toBe(1);
});

test("audit filters form a native complete download and interrupted transfers fail", async ({
  page,
}) => {
  await managementFixture(page);
  let fail = false;
  const event =
    JSON.stringify({
      id: "audit",
      action: "identity.update",
      result: "succeeded",
    }) + "\n";
  const server = createServer((req, response) => {
    response.writeHead(200, {
      "Content-Type": "application/x-ndjson",
      "Content-Disposition": 'attachment; filename="audit-events.ndjson"',
    });
    response.write(event);
    if (fail) {
      const timer = setTimeout(() => response.destroy(), 100);
      response.on("close", () => clearTimeout(timer));
    } else response.end(event);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    const address = server.address();
    if (!address || typeof address === "string")
      throw new Error("Missing test listener");
    await page.route("**/dans/audit-events/export?*", (route) =>
      route.continue({ url: "http://127.0.0.1:" + address.port + "/export" }),
    );
    await page.goto("/console/#/audit");
    await page.getByLabel("Actor ID", { exact: true }).fill("actor");
    await page.getByLabel("Action", { exact: true }).fill("identity.update");
    await page.getByLabel("Target type", { exact: true }).fill("identity");
    await page.getByLabel("Target ID", { exact: true }).fill("member");
    await page
      .getByRole("button", { name: "Filter audit", exact: true })
      .click();
    const link = page.getByRole("link", {
      name: "Download NDJSON",
      exact: true,
    });
    await expect(link).toHaveAttribute("href", /actor_id=actor/);
    const completed = page.waitForEvent("download");
    await link.click();
    const download = await completed;
    expect(await download.failure()).toBeNull();
    expect(download.suggestedFilename()).toBe("audit-events.ndjson");
    const stream = await download.createReadStream();
    let bytes = "";
    for await (const chunk of stream) bytes += chunk.toString();
    expect(bytes).toBe(event + event);
    await expect(page.getByText(/Download requested/)).toBeVisible();
    fail = true;
    const interrupted = page.waitForEvent("download");
    await link.click();
    expect(await (await interrupted).failure()).toBeTruthy();
    await expect(
      page.getByText(
        /Check your browser’s downloads for completion or failure/,
      ),
    ).toBeVisible();
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});

test("management keyboard focus and narrow forms preserve complete content", async ({
  page,
}) => {
  await managementFixture(page);
  await page.goto("/console/#/groups");
  const button = page.getByRole("button", {
    name: "Create group",
    exact: true,
  });
  await button.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(button).toBeFocused();
  await page.setViewportSize({ width: 320, height: 740 });
  await button.click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Display name", { exact: true })
    .fill("Long synthetic multilingual label مثال ".repeat(10));
  await expect(dialog.getByLabel("Handle", { exact: true })).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Create group", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBe(true);
  await page.screenshot({
    path: test.info().outputPath("management-narrow.png"),
    fullPage: true,
  });
});

test("failed collection read can be explicitly reloaded without leaving the page", async ({
  page,
}) => {
  await managementFixture(page);
  let failed = true;
  await page.route("**/dans/me/tokens?*", (route) =>
    failed
      ? route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: "Temporary read failure" }),
        })
      : route.fallback(),
  );
  await page.goto("/console/#/me");
  await page.getByRole("tab", { name: "API tokens", exact: true }).click();
  await expect(
    page.getByText("Temporary read failure", { exact: true }),
  ).toBeVisible();
  failed = false;
  await page
    .getByRole("button", { name: "Retry loading", exact: true })
    .click();
  await expect(
    page.getByText("Current sign-in", { exact: true }),
  ).toBeVisible();
});

test("async identity details receive heading focus after navigation", async ({
  page,
}) => {
  await managementFixture(page);
  await page.goto("/console/#/identities");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await page.getByRole("link", { name: "team_member", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: "team_member", exact: true }),
  ).toBeFocused();
});

test("a partial authority page never becomes a false DNS denial", async ({
  page,
}) => {
  const state = await managementFixture(page);
  const zone = { id: "example.test.", name: "example.test.", kind: "Native" };
  const rrset = {
    name: "www.example.test.",
    type: "A",
    ttl: 300,
    records: [{ content: "192.0.2.1", disabled: false }],
  };
  await page.route("**/dans/me", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ...actor, operator: false }),
    }),
  );
  await page.route("**/dans/me/delegations?*", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [], next_cursor: "more-grants" }),
    }),
  );
  await page.route("**/servers/localhost/zones", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([zone]),
    }),
  );
  await page.route("**/dans/servers/**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [rrset],
        next_cursor: null,
        state: "ready",
        last_refreshed_at: "2026-01-01T00:00:00Z",
        error: null,
      }),
    }),
  );
  await page.goto("/console/#/zones");
  await page
    .getByRole("link", { name: zone.name, exact: true })
    .first()
    .click();
  await expect(
    page.getByRole("button", { name: "Edit www.example.test. A", exact: true }),
  ).toBeEnabled();
  await expect(
    page.getByRole("button", {
      name: "Delete www.example.test. A",
      exact: true,
    }),
  ).toBeEnabled();
  await page
    .getByRole("button", { name: "Your editing permissions", exact: true })
    .click();
  await expect(
    page.getByText(/Only the first page of your authority/),
  ).toBeVisible();
  expect(
    state.requests.some((r) => r.query.get("cursor") === "more-grants"),
  ).toBe(false);
});

test("native 200 percent zoom keeps management forms and confirmations usable", async ({
  browserName,
  baseURL,
}) => {
  test.skip(
    browserName !== "chromium",
    "Native zoom preference verification uses Chromium; WebKit reflow is checked separately.",
  );
  const profile = await mkdtemp(join(tmpdir(), "management-zoom-"));
  try {
    await mkdir(join(profile, "Default"));
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
      baseURL,
      args: ["--window-size=1280,960"],
    });
    try {
      const page = context.pages()[0];
      await managementFixture(page);
      await page.goto("/console/#/identities/actor");
      expect(
        await page.evaluate(() => ({
          width: innerWidth,
          scale: devicePixelRatio,
        })),
      ).toEqual({ width: 640, scale: 2 });
      await page
        .getByRole("button", { name: "Edit profile", exact: true })
        .click();
      await expect(
        page.getByRole("dialog").getByLabel("Display name", { exact: true }),
      ).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(
        page.getByRole("button", { name: "Edit profile", exact: true }),
      ).toBeFocused();
      await page
        .getByRole("button", { name: "Disable identity", exact: true })
        .click();
      await expect(
        page
          .getByRole("dialog")
          .getByRole("button", { name: "Disable identity", exact: true }),
      ).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBe(true);
      // CDP captures the current frame, including an unfinished modal fade.
      await page.evaluate(async () => {
        const finiteAnimations = document
          .getAnimations()
          .filter(
            (animation) =>
              animation.effect?.getComputedTiming().iterations !== Infinity,
          );
        await Promise.all(
          finiteAnimations.map((animation) =>
            animation.finished.catch(() => {}),
          ),
        );
        await new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        );
      });
      const cdp = await context.newCDPSession(page);
      const capture = await cdp.send("Page.captureScreenshot", {
        format: "png",
        captureBeyondViewport: false,
      });
      await writeFile(
        test.info().outputPath("management-native-zoom.png"),
        Buffer.from(capture.data, "base64"),
      );
      await cdp.detach();
    } finally {
      await context.close();
    }
  } finally {
    await rm(profile, { recursive: true, force: true });
  }
});

test("group creation, profile, state changes and membership removal remain explicit", async ({
  page,
}) => {
  const state = await managementFixture(page);
  await page.goto("/console/#/groups");
  await page.getByRole("button", { name: "Create group", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Handle", { exact: true }).fill("new-team");
  await dialog.getByLabel("Display name", { exact: true }).fill("New team");
  await dialog
    .getByRole("button", { name: "Create group", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  expect(
    state.requests.filter(
      (r) => r.method === "POST" && r.path === "/dans/groups",
    ),
  ).toHaveLength(1);
  await page.getByRole("button", { name: "Edit group", exact: true }).click();
  await dialog.getByLabel("Display name", { exact: true }).fill("Edited team");
  await dialog
    .getByRole("button", { name: "Save profile", exact: true })
    .click();
  await expect(page.getByText("Edited team", { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: "Disable group", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Disable group", exact: true })
    .click();
  await page.getByRole("button", { name: "Enable group", exact: true }).click();
  await expect(dialog).toContainText("restore eligible access");
  await dialog
    .getByRole("button", { name: "Enable group", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Disable group", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Disable group", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Enable group", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Remove team_member", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Remove member", exact: true })
    .click();
  expect(
    state.requests.filter(
      (r) => r.method === "DELETE" && r.path.endsWith("/members/member"),
    ),
  ).toHaveLength(1);
});

test("binding creation, absence confirmation and rebind submit their distinct contracts", async ({
  page,
}) => {
  const state = await managementFixture(page);
  await page.route("**/dans/zone-bindings", (route) =>
    route.request().method() === "POST"
      ? route.fulfill({
          status: 201,
          contentType: "application/json",
          body: JSON.stringify({
            id: "binding",
            zone_id: "example.test.",
            zone_name: "example.test.",
            status: "active",
            generation: 1,
          }),
        })
      : route.fallback(),
  );
  await page.goto("/console/#/bindings");
  await page
    .getByRole("button", { name: "Create binding", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Zone ID", { exact: true }).fill("example.test.");
  await expect(dialog).toContainText("current zone lifetime");
  await dialog
    .getByRole("button", { name: "Create binding", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await page
    .getByRole("button", { name: "Confirm absence", exact: true })
    .click();
  await expect(dialog).toContainText("API rejects this action");
  await dialog
    .getByRole("button", { name: "Confirm absence", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await page.getByRole("button", { name: "Rebind zone", exact: true }).click();
  await expect(dialog).toContainText("does not copy or restore");
  await dialog
    .getByRole("button", { name: "Rebind zone", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  expect(
    state.requests.filter((r) => r.path.endsWith("/confirm-absent")),
  ).toHaveLength(1);
  expect(state.requests.find((r) => r.path.endsWith("/rebind"))?.body).toEqual({
    zone_id: "example.test.",
  });
});

test("retained authority identifies suspended groups and permanently retired lifetimes", async ({
  page,
}) => {
  await managementFixture(page);
  await page.route("**/dans/identities/member", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ...member, enabled: true }),
    }),
  );
  const base = {
    id: "assignment",
    zone_binding_id: "binding",
    zone_id: "example.test.",
    zone_name: "example.test.",
    grantee_kind: "group",
    grantee_id: "group",
    selectors: [{ kind: "exact", value: "www.example.test." }],
    record_types: null,
    change_kinds: null,
    created_at: "2026-01-01T00:00:00Z",
    revoked_at: null,
    identity_enabled: true,
    group_enabled: false,
    group_handle: "team_group",
    binding_status: "active",
    effective: false,
  };
  await page.route("**/dans/identities/member/assignments?*", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [base, { ...base, id: "retired", binding_status: "retired" }],
        next_cursor: null,
      }),
    }),
  );
  await page.goto("/console/#/identities/member");
  await page
    .getByRole("tab", { name: "Retained assignments", exact: true })
    .click();
  await expect(
    page.getByText("Suspended · group disabled", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Retired zone lifetime · permanently inactive", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Group: team_group", { exact: true }),
  ).toHaveCount(2);
  await page.screenshot({
    path: test.info().outputPath("retained-authority.png"),
    fullPage: true,
  });
});

test("clipboard failure preserves a selectable one-time token and clear recovery", async ({
  page,
}) => {
  await managementFixture(page);
  await page.addInitScript(() =>
    Object.defineProperty(navigator, "clipboard", {
      value: {
        writeText: () => Promise.reject(new Error("Permission denied")),
      },
      configurable: true,
    }),
  );
  await page.goto("/console/#/me");
  await page.getByRole("tab", { name: "API tokens", exact: true }).click();
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Label", { exact: true }).fill("copy-test");
  await dialog
    .getByRole("button", { name: "Create token", exact: true })
    .click();
  await page.getByRole("button", { name: "Copy token", exact: true }).click();
  await expect(
    page.getByText(/Clipboard access was unavailable/),
  ).toBeVisible();
  await expect(
    page.getByText("dans_v1_once_only_synthetic_secret", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expect(
    page.getByText("dans_v1_once_only_synthetic_secret", { exact: true }),
  ).toHaveCount(0);
});
