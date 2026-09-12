import { useEffect, useState } from "react";
import AppLayout from "@cloudscape-design/components/app-layout";
import Alert from "@cloudscape-design/components/alert";
import BreadcrumbGroup from "@cloudscape-design/components/breadcrumb-group";
import Button from "@cloudscape-design/components/button";
import Container from "@cloudscape-design/components/container";
import Form from "@cloudscape-design/components/form";
import Header from "@cloudscape-design/components/header";
import SideNavigation from "@cloudscape-design/components/side-navigation";
import SpaceBetween from "@cloudscape-design/components/space-between";
import Spinner from "@cloudscape-design/components/spinner";
import { request, APIError } from "./api.ts";
import { allPages, Failure, Field, messageOf } from "./controls.tsx";
import type { Identity, Delegation } from "./domain.ts";
import { Zones } from "./Zones.tsx";
import { Administration } from "./Administration.tsx";
export function App() {
  const [identity, setIdentity] = useState<Identity | null>(null),
    [grants, setGrants] = useState<Delegation[]>([]),
    [booting, setBooting] = useState(true),
    [token, setToken] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [route, setRoute] = useState(location.hash || "#/zones");
  async function recover() {
    const actor = await request<Identity>("/dans/me");
    const items = await allPages<Delegation>("/dans/me/delegations");
    setIdentity(actor);
    setGrants(items);
  }
  useEffect(() => {
    recover()
      .catch((error) => {
        if (!(error instanceof APIError && error.status === 401))
          setError(messageOf(error));
      })
      .finally(() => setBooting(false));
    const unauthorized = () => {
      setIdentity(null);
      setGrants([]);
      setNotice("");
      setError("Your session ended. Sign in again.");
    };
    const navigate = () => setRoute(location.hash || "#/zones");
    window.addEventListener("hashchange", navigate);
    window.addEventListener("console:unauthorized", unauthorized);
    return () => {
      window.removeEventListener("hashchange", navigate);
      window.removeEventListener("console:unauthorized", unauthorized);
    };
  }, []);
  useEffect(() => {
    document.title =
      (route.startsWith("#/audit")
        ? "Audit"
        : route.startsWith("#/delegations")
          ? "Delegations"
          : "Zones") + " · DANS";
    requestAnimationFrame(() => {
      const heading = document.querySelector<HTMLElement>("main h1");
      if (heading) {
        heading.tabIndex = -1;
        heading.focus();
      }
    });
  }, [route]);
  async function login() {
    if (busy) return;
    setBusy(true);
    setError("");
    const supplied = token;
    setToken("");
    try {
      await request("/dans/session", {
        method: "POST",
        body: JSON.stringify({ token: supplied }),
      });
      await recover();
      location.hash = "#/zones";
    } catch (error) {
      setError(messageOf(error));
    } finally {
      setBusy(false);
    }
  }
  async function logout() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await request("/dans/session", { method: "DELETE" });
    } catch {
      setError(
        "Sign-out could not be confirmed. Protected content has been cleared. Revoke the original API token to end all of its sessions immediately; otherwise this session expires within seven days of sign-in.",
      );
    } finally {
      setIdentity(null);
      setGrants([]);
      setNotice("");
      setToken("");
      setBusy(false);
    }
  }
  const section = route.startsWith("#/delegations")
    ? "Delegations"
    : route.startsWith("#/audit")
      ? "Audit"
      : "Zones";
  return (
    <>
      <a
        className="skip-link"
        href="#main-content"
        onClick={(event) => {
          event.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
      >
        Skip to content
      </a>
      <header className="topbar">
        <a href="#/zones" className="brand">
          DANS <span>DNS console</span>
        </a>
        {identity && (
          <div className="session">
            <span>
              {identity.display_name || identity.handle} ·{" "}
              {identity.operator ? "DANS operator" : "Delegated user"}
            </span>
            <Button loading={busy} onClick={logout}>
              Sign out
            </Button>
          </div>
        )}
      </header>
      {!identity ? (
        <main id="main-content" className="signin" tabIndex={-1}>
          <Container header={<Header variant="h1">Sign in to DANS</Header>}>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void login();
              }}
            >
              <Form
                actions={
                  <Button
                    variant="primary"
                    formAction="submit"
                    loading={busy}
                    disabled={booting}
                  >
                    Sign in
                  </Button>
                }
              >
                <SpaceBetween size="l">
                  <p>
                    Manage DNS zones, complete RRsets, and delegated access.
                  </p>
                  {booting ? (
                    <Spinner size="normal" />
                  ) : (
                    <>
                      <Field
                        label="API token"
                        type="password"
                        value={token}
                        onChange={setToken}
                        description="Use an existing API token. Your browser remembers this sign-in for up to seven days."
                      />
                      <Failure error={error} />
                    </>
                  )}
                </SpaceBetween>
              </Form>
            </form>
          </Container>
        </main>
      ) : (
        <AppLayout
          toolsHide
          navigation={
            <SideNavigation
              activeHref={"#/" + section.toLowerCase()}
              header={{ text: "DNS management", href: "#/zones" }}
              items={[
                { type: "link", text: "Zones", href: "#/zones" },
                ...(identity.operator
                  ? [
                      {
                        type: "link" as const,
                        text: "Delegations",
                        href: "#/delegations",
                      },
                      { type: "link" as const, text: "Audit", href: "#/audit" },
                    ]
                  : []),
              ]}
            />
          }
          breadcrumbs={
            <BreadcrumbGroup
              items={[
                { text: "DANS", href: "#/zones" },
                { text: section, href: "#/" + section.toLowerCase() },
              ]}
            />
          }
          content={
            <div id="main-content" tabIndex={-1}>
              <SpaceBetween size="l">
                {notice && (
                  <Alert
                    dismissible
                    dismissAriaLabel="Dismiss notification"
                    onDismiss={() => setNotice("")}
                    type="success"
                  >
                    {notice}
                  </Alert>
                )}
                <Failure error={error} />
                {section === "Zones" ? (
                  <Zones
                    route={route}
                    identity={identity}
                    grants={grants}
                    notify={setNotice}
                  />
                ) : identity.operator ? (
                  <Administration section={section} notify={setNotice} />
                ) : (
                  <Alert type="error">
                    DANS operator authority is required.
                  </Alert>
                )}
              </SpaceBetween>
            </div>
          }
          ariaLabels={{
            navigation: "Navigation",
            navigationClose: "Close navigation",
            navigationToggle: "Open navigation",
          }}
        />
      )}
    </>
  );
}
