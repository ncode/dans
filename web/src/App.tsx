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
import { Failure, Field, messageOf } from "./controls.tsx";
import type { Identity, Delegation, Page } from "./domain.ts";
import { Zones } from "./Zones.tsx";
import { Administration } from "./Administration.tsx";
import { Management } from "./Management.tsx";
import { Bindings } from "./Bindings.tsx";
const sections: Record<string, string> = {
  zones: "Zones",
  delegations: "Delegations",
  audit: "Audit",
  identities: "Identities",
  groups: "Groups",
  bindings: "Zone bindings",
  me: "My access",
};
export function App() {
  const [identity, setIdentity] = useState<Identity | null>(null),
    [grants, setGrants] = useState<Delegation[]>([]),
    [grantsComplete, setGrantsComplete] = useState(false),
    [currentToken, setCurrentToken] = useState(""),
    [booting, setBooting] = useState(true),
    [token, setToken] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [route, setRoute] = useState(location.hash || "#/zones");
  async function recover() {
    const actor = await request<Identity>("/dans/me");
    const [page, credential] = await Promise.all([
      request<Page<Delegation>>("/dans/me/delegations?limit=100"),
      request<{ token_id: string }>("/dans/me/credential"),
    ]);
    setIdentity(actor);
    setGrants(page.items);
    setGrantsComplete(!page.next_cursor);
    setCurrentToken(credential.token_id);
  }
  function endSession() {
    setIdentity(null);
    setGrants([]);
    setGrantsComplete(false);
    setCurrentToken("");
    setNotice("");
    setToken("");
    setError("Your session ended. Sign in again.");
  }
  async function refreshActor() {
    try {
      await recover();
      setError("");
    } catch (error) {
      setError(messageOf(error));
    }
  }
  useEffect(() => {
    recover()
      .catch((error) => {
        if (!(error instanceof APIError && error.status === 401))
          setError(messageOf(error));
      })
      .finally(() => setBooting(false));
    const unauthorized = endSession;
    const navigate = () => setRoute(location.hash || "#/zones");
    window.addEventListener("hashchange", navigate);
    window.addEventListener("console:unauthorized", unauthorized);
    return () => {
      window.removeEventListener("hashchange", navigate);
      window.removeEventListener("console:unauthorized", unauthorized);
    };
  }, []);
  useEffect(() => {
    document.title = (sections[route.split("/")[1]] || "Zones") + " · DANS";
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
      setGrantsComplete(false);
      setCurrentToken("");
      setNotice("");
      setToken("");
      setBusy(false);
    }
  }
  const sectionKey = sections[route.split("/")[1]]
    ? route.split("/")[1]
    : "zones";
  const section = sections[sectionKey];
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
              activeHref={"#/" + sectionKey}
              header={{ text: "DNS management", href: "#/zones" }}
              items={[
                { type: "link", text: "Zones", href: "#/zones" },
                { type: "link", text: "My access", href: "#/me" },
                ...(identity.operator
                  ? [
                      {
                        type: "link" as const,
                        text: "Identities",
                        href: "#/identities",
                      },
                      {
                        type: "link" as const,
                        text: "Groups",
                        href: "#/groups",
                      },
                      {
                        type: "link" as const,
                        text: "Zone bindings",
                        href: "#/bindings",
                      },
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
                { text: section, href: "#/" + sectionKey },
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
                    grantsComplete={grantsComplete}
                    notify={setNotice}
                  />
                ) : sectionKey === "me" ||
                  (identity.operator &&
                    ["identities", "groups"].includes(sectionKey)) ? (
                  <Management
                    route={route}
                    actor={identity}
                    currentToken={currentToken}
                    notify={setNotice}
                    refreshActor={refreshActor}
                    endSession={endSession}
                  />
                ) : identity.operator ? (
                  sectionKey === "bindings" ? (
                    <Bindings route={route} notify={setNotice} />
                  ) : (
                    <Administration
                      key={section}
                      section={section}
                      notify={setNotice}
                    />
                  )
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
