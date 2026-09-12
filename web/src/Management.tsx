import { useEffect, useState } from "react";
import Alert from "@cloudscape-design/components/alert";
import Button from "@cloudscape-design/components/button";
import Container from "@cloudscape-design/components/container";
import Header from "@cloudscape-design/components/header";
import Link from "@cloudscape-design/components/link";
import Modal from "@cloudscape-design/components/modal";
import SpaceBetween from "@cloudscape-design/components/space-between";
import Spinner from "@cloudscape-design/components/spinner";
import Table from "@cloudscape-design/components/table";
import Tabs from "@cloudscape-design/components/tabs";
import { request } from "./api.ts";
import { Choice, Failure, Field } from "./controls.tsx";
import {
  ConfirmAction,
  managementError,
  managementPath,
  queryPath,
  RemoteSelector,
  usePage,
  useDetailHeading,
} from "./management-controls.tsx";
import type {
  Assignment,
  Delegation,
  Group,
  Identity,
  Token,
} from "./domain.ts";

type ManagementProps = {
  route: string;
  actor: Identity;
  currentToken: string;
  notify: (message: string) => void;
  refreshActor: () => Promise<void>;
  endSession: () => void;
};
export function Management(props: ManagementProps) {
  const [, section, encoded] = props.route.split("/");
  let id = "";
  try {
    id = decodeURIComponent(encoded || "");
  } catch {
    /* Show collection for malformed route. */
  }
  if (section === "me")
    return <IdentityDetails key="me" {...props} id={props.actor.id} self />;
  if (section === "identities" && id)
    return <IdentityDetails key={id} {...props} id={id} />;
  if (section === "groups" && id)
    return <GroupDetails key={id} {...props} id={id} />;
  return (
    <ResourceList
      key={section}
      kind={section === "groups" ? "groups" : "identities"}
      notify={props.notify}
    />
  );
}

function ResourceList({
  kind,
  notify,
}: {
  kind: "identities" | "groups";
  notify: (message: string) => void;
}) {
  const [prefix, setPrefix] = useState(""),
    [identityKind, setIdentityKind] = useState(""),
    [enabled, setEnabled] = useState(""),
    [filters, setFilters] = useState({
      handle_prefix: "",
      kind: "",
      enabled: "",
    });
  const [create, setCreate] = useState(false);
  const page = usePage<Identity | Group>(queryPath("/dans/" + kind, filters));
  const identities = kind === "identities",
    title = identities ? "Identities" : "Groups";
  return (
    <SpaceBetween size="l">
      <Failure error={page.error} onRetry={page.reload} />
      <Table
        variant="full-page"
        trackBy="id"
        wrapLines
        items={page.items}
        loading={page.loading}
        loadingText={"Loading " + kind}
        header={
          <Header
            variant="h1"
            description={
              identities
                ? "Users and service identities. Handles and kinds are permanent."
                : "Direct, non-nested membership. Disabled groups retain their assignments."
            }
            actions={
              <Button variant="primary" onClick={() => setCreate(true)}>
                Create {identities ? "identity" : "group"}
              </Button>
            }
          >
            {title}
          </Header>
        }
        filter={
          <div className="filter-grid">
            <Field label="Handle prefix" value={prefix} onChange={setPrefix} />
            {identities && (
              <Choice
                label="Identity kind"
                value={identityKind}
                onChange={setIdentityKind}
                options={[
                  { value: "", label: "All kinds" },
                  { value: "user", label: "User" },
                  { value: "service", label: "Service identity" },
                ]}
              />
            )}
            <Choice
              label="Enabled state"
              value={enabled}
              onChange={setEnabled}
              options={[
                { value: "", label: "All states" },
                { value: "true", label: "Enabled" },
                { value: "false", label: "Disabled" },
              ]}
            />
            <Button
              onClick={() => {
                setFilters({
                  handle_prefix: prefix,
                  kind: identities ? identityKind : "",
                  enabled,
                });
                page.reload();
              }}
            >
              Search {kind}
            </Button>
          </div>
        }
        columnDefinitions={[
          {
            id: "handle",
            header: "Handle",
            cell: (item) => (
              <Link href={"#/" + kind + "/" + encodeURIComponent(item.id)}>
                {item.handle}
              </Link>
            ),
          },
          {
            id: "name",
            header: "Display name",
            cell: (item) => item.display_name || "—",
          },
          ...(identities
            ? [
                {
                  id: "kind",
                  header: "Kind",
                  cell: (item: Identity | Group) =>
                    (item as Identity).kind === "service"
                      ? "Service identity"
                      : "User",
                },
              ]
            : []),
          {
            id: "state",
            header: "State",
            cell: (item) => (item.enabled ? "Enabled" : "Disabled"),
          },
          ...(identities
            ? [
                {
                  id: "operator",
                  header: "DANS operator",
                  cell: (item: Identity | Group) =>
                    (item as Identity).operator ? "Assigned" : "No",
                },
              ]
            : []),
        ]}
        pagination={page.pagination}
        empty={"No " + kind + " match these filters."}
      />
      {create && (
        <ResourceForm
          kind={kind}
          close={() => setCreate(false)}
          done={(item) => {
            setCreate(false);
            notify(
              (identities ? "Identity" : "Group") +
                " created. Token creation is a separate action.",
            );
            location.hash = "#/" + kind + "/" + encodeURIComponent(item.id);
          }}
        />
      )}
    </SpaceBetween>
  );
}

function ResourceForm({
  kind,
  resource,
  close,
  done,
}: {
  kind: "identities" | "groups";
  resource?: Identity | Group;
  close: () => void;
  done: (item: Identity | Group) => void;
}) {
  const [handle, setHandle] = useState(""),
    [name, setName] = useState(resource?.display_name || ""),
    [identityKind, setKind] = useState("user"),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const title =
    (resource ? "Edit " : "Create ") +
    (kind === "identities" ? "identity" : "group");
  async function save() {
    if (busy) return;
    if (!resource && !/^[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?$/.test(handle)) {
      setError(
        "Use a lowercase handle of 1–63 letters, digits, dots, underscores or hyphens, starting and ending with a letter or digit.",
      );
      return;
    }
    setBusy(true);
    setError("");
    try {
      const item = await request<Identity | Group>(
        resource ? managementPath(kind, resource.id) : "/dans/" + kind,
        {
          method: resource ? "PATCH" : "POST",
          body: JSON.stringify({
            display_name: name || null,
            ...(!resource
              ? {
                  handle,
                  ...(kind === "identities" ? { kind: identityKind } : {}),
                }
              : {}),
          }),
        },
      );
      if (!item?.id)
        throw new Error(
          "The change succeeded, but its details could not be read. Close this form and reload the collection before another write.",
        );
      done(item);
    } catch (error) {
      setError(managementError(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      visible
      header={title}
      closeAriaLabel={"Close " + title.toLowerCase()}
      onDismiss={() => {
        if (!busy) close();
      }}
      footer={
        <SpaceBetween direction="horizontal" size="xs">
          <Button disabled={busy} onClick={close}>
            Cancel
          </Button>
          <Button variant="primary" loading={busy} onClick={save}>
            {resource ? "Save profile" : title}
          </Button>
        </SpaceBetween>
      }
    >
      <SpaceBetween size="m">
        <Failure error={error} />
        {!resource && (
          <>
            <Field
              label="Handle"
              value={handle}
              onChange={setHandle}
              description="Immutable and reserved even when disabled."
            />
            {kind === "identities" && (
              <Choice
                label="Identity kind"
                value={identityKind}
                onChange={setKind}
                options={[
                  { value: "user", label: "User" },
                  { value: "service", label: "Service identity" },
                ]}
              />
            )}
          </>
        )}
        <Field
          label="Display name"
          value={name}
          onChange={setName}
          description="Optional, non-unique label."
        />
      </SpaceBetween>
    </Modal>
  );
}

function IdentityDetails({
  id,
  self = false,
  actor,
  currentToken,
  notify,
  refreshActor,
  endSession,
}: ManagementProps & { id: string; self?: boolean }) {
  const [identity, setIdentity] = useState<Identity | null>(
      self ? actor : null,
    ),
    [error, setError] = useState(""),
    [editing, setEditing] = useState(false),
    [tab, setTab] = useState("profile"),
    [change, setChange] = useState<{
      title: string;
      patch: Record<string, boolean>;
      message: string;
    } | null>(null);
  async function load() {
    try {
      setIdentity(
        await request<Identity>(
          self ? "/dans/me" : managementPath("identities", id),
        ),
      );
      setError("");
    } catch (error) {
      setError(managementError(error));
    }
  }
  useEffect(() => {
    void load();
  }, [id, self]);
  useDetailHeading(
    identity ? (self ? "My access" : identity.handle) : undefined,
  );
  if (!identity)
    return (
      <SpaceBetween size="m">
        <Failure error={error} />
        <Button onClick={load}>Reload identity</Button>
        {!error && <Spinner />}
      </SpaceBetween>
    );
  const mine = id === actor.id,
    path = self ? "/dans/me" : managementPath("identities", id);
  const profile = (
    <SpaceBetween size="m">
      <dl className="details">
        <dt>Handle</dt>
        <dd>{identity.handle}</dd>
        <dt>Kind</dt>
        <dd>{identity.kind === "service" ? "Service identity" : "User"}</dd>
        <dt>Display name</dt>
        <dd>{identity.display_name || "—"}</dd>
        <dt>State</dt>
        <dd>{identity.enabled ? "Enabled" : "Disabled"}</dd>
        <dt>DANS operator role</dt>
        <dd>
          {identity.operator
            ? "Assigned" +
              (!identity.enabled
                ? " · suspended while disabled"
                : " · full management and DNS authority")
            : "Not assigned"}
        </dd>
        <dt>Identity ID</dt>
        <dd>{identity.id}</dd>
      </dl>
      {self && actor.operator && (
        <Link href={"#/identities/" + encodeURIComponent(id)}>
          Manage your identity and access
        </Link>
      )}
      {!self && (
        <SpaceBetween direction="horizontal" size="xs">
          <Button onClick={() => setEditing(true)}>Edit profile</Button>
          <Button
            onClick={() =>
              setChange({
                title: identity.enabled
                  ? "Disable identity"
                  : "Enable identity",
                patch: { enabled: !identity.enabled },
                message: identity.enabled
                  ? "Disable this identity? Its tokens and memberships are retained, but it loses all usable authority. " +
                    (mine
                      ? "Your sign-in and other sessions for this identity will end."
                      : "")
                  : "Re-enable this identity? This can restore access through retained unexpired tokens, memberships, delegations and its DANS operator role. Revoked grants and retired zone lifetimes are never restored.",
              })
            }
          >
            {identity.enabled ? "Disable identity" : "Enable identity"}
          </Button>
          <Button
            onClick={() =>
              setChange({
                title: identity.operator
                  ? "Remove DANS operator role"
                  : "Assign DANS operator role",
                patch: { operator: !identity.operator },
                message: identity.operator
                  ? "Remove full DNS and management authority? Eligible delegations remain. " +
                    (mine
                      ? "You will leave the management pages and keep self-service access. "
                      : "") +
                    "The API prevents removing the last enabled DANS operator."
                  : "Assign full DNS and access-management authority? If this identity is disabled, the role becomes usable when it is re-enabled.",
              })
            }
          >
            {identity.operator
              ? "Remove DANS operator role"
              : "Assign DANS operator role"}
          </Button>
        </SpaceBetween>
      )}
    </SpaceBetween>
  );
  return (
    <SpaceBetween size="l">
      <Header
        variant="h1"
        description={
          self
            ? "Your identity, memberships, effective authority and API tokens."
            : "Identity details"
        }
        actions={
          <Button
            onClick={async () => {
              await load();
              if (mine) await refreshActor();
            }}
          >
            Reload profile
          </Button>
        }
      >
        {self ? "My access" : identity.handle}
      </Header>
      <Failure error={error} />
      {!identity.enabled && (
        <Alert type="warning">
          Disabled identities have no usable authority. Memberships, tokens and
          assignments remain inspectable.
        </Alert>
      )}
      <Tabs
        activeTabId={tab}
        onChange={({ detail }) => setTab(detail.activeTabId)}
        tabs={[
          { id: "profile", label: "Profile", content: profile },
          {
            id: "groups",
            label: "Groups",
            content: tab === "groups" && (
              <Memberships path={path + "/groups"} operator={actor.operator} />
            ),
          },
          {
            id: "authority",
            label: "Effective authority now",
            content: tab === "authority" && (
              <Authority path={path + "/delegations"} identity={identity} />
            ),
          },
          ...(!self
            ? [
                {
                  id: "assignments",
                  label: "Retained assignments",
                  content: tab === "assignments" && (
                    <Authority
                      path={path + "/assignments"}
                      identity={identity}
                      retained
                    />
                  ),
                },
              ]
            : []),
          {
            id: "tokens",
            label: "API tokens",
            content: tab === "tokens" && (
              <Tokens
                path={path + "/tokens"}
                currentToken={mine ? currentToken : ""}
                notify={notify}
                endSession={endSession}
              />
            ),
          },
        ]}
      />
      {editing && (
        <ResourceForm
          kind="identities"
          resource={identity}
          close={() => setEditing(false)}
          done={(item) => {
            setIdentity(item as Identity);
            setEditing(false);
            notify("Identity profile saved.");
            if (mine) void refreshActor();
          }}
        />
      )}
      {change && (
        <ConfirmAction
          title={change.title}
          close={() => setChange(null)}
          action={async () => {
            await request(managementPath("identities", id), {
              method: "PATCH",
              body: JSON.stringify(change.patch),
            });
          }}
          done={() => {
            const patch = change.patch;
            setChange(null);
            notify("Identity access updated.");
            if (mine && patch.enabled === false) {
              endSession();
              return;
            }
            void load();
            if (mine) {
              void refreshActor();
              if (patch.operator === false) location.hash = "#/me";
            }
          }}
        >
          {change.message}
        </ConfirmAction>
      )}
    </SpaceBetween>
  );
}

function Memberships({ path, operator }: { path: string; operator: boolean }) {
  const [prefix, setPrefix] = useState(""),
    [filter, setFilter] = useState("");
  const page = usePage<Group>(queryPath(path, { handle_prefix: filter }));
  return (
    <SpaceBetween size="m">
      <Failure error={page.error} onRetry={page.reload} />
      <Table
        trackBy="id"
        wrapLines
        items={page.items}
        loading={page.loading}
        loadingText="Loading memberships"
        header={<Header>Group memberships</Header>}
        filter={
          <div className="filter-grid">
            <Field
              label="Group handle prefix"
              value={prefix}
              onChange={setPrefix}
            />
            <Button
              onClick={() => {
                setFilter(prefix);
                page.reload();
              }}
            >
              Search groups
            </Button>
          </div>
        }
        columnDefinitions={[
          {
            id: "handle",
            header: "Group",
            cell: (item) =>
              operator ? (
                <Link href={"#/groups/" + item.id}>{item.handle}</Link>
              ) : (
                item.handle
              ),
          },
          {
            id: "name",
            header: "Display name",
            cell: (item) => item.display_name || "—",
          },
          {
            id: "state",
            header: "Authority contribution",
            cell: (item) =>
              item.enabled
                ? "Enabled group; eligible assignments may apply"
                : "Disabled group; no effective authority",
          },
        ]}
        pagination={page.pagination}
        empty="No group memberships match."
      />
    </SpaceBetween>
  );
}

function Authority({
  path,
  identity,
  retained = false,
}: {
  path: string;
  identity: Identity;
  retained?: boolean;
}) {
  const page = usePage<Delegation | Assignment>(path);
  return (
    <SpaceBetween size="m">
      <Alert type={identity.enabled ? "info" : "warning"}>
        {!identity.enabled
          ? "No usable authority while this identity is disabled."
          : identity.operator
            ? "The DANS operator role grants full DNS and management authority, independently of the delegation list."
            : "Effective delegations combine direct assignments and enabled group memberships."}{" "}
        {retained
          ? "Retained assignments include suspended access. Revoked grants and retired zone lifetimes cannot be restored by re-enabling an identity or group."
          : "This is a paginated view of current authority; the API checks each request."}
      </Alert>
      <Failure error={page.error} onRetry={page.reload} />
      <Table
        wrapLines
        trackBy="id"
        items={page.items}
        loading={page.loading}
        loadingText="Loading authority"
        header={
          <Header>
            {retained ? "Retained assignments" : "Effective delegations"}
          </Header>
        }
        columnDefinitions={[
          {
            id: "zone",
            header: "Zone",
            cell: (item) =>
              item.zone_name || item.zone_id || item.zone_binding_id,
          },
          {
            id: "source",
            header: "Source",
            cell: (item) =>
              item.grantee_kind === "group"
                ? "Group: " +
                  ((item as Assignment).group_handle || item.grantee_id)
                : "Direct identity assignment",
          },
          {
            id: "selectors",
            header: "Selectors",
            cell: (item) =>
              item.selectors.map((s) => (
                <div key={s.kind + s.value} className="dns-value">
                  {s.kind}: {s.value}
                </div>
              )),
          },
          {
            id: "types",
            header: "Restrictions",
            cell: (item) => (
              <>
                {item.record_types?.join(", ") || "All record types"}
                <br />
                {item.change_kinds?.join(", ") || "All change kinds"}
              </>
            ),
          },
          ...(retained
            ? [
                {
                  id: "state",
                  header: "State",
                  cell: (item: Delegation | Assignment) => {
                    const grant = item as Assignment;
                    return grant.binding_status === "retired"
                      ? "Retired zone lifetime · permanently inactive"
                      : grant.revoked_at
                        ? "Revoked · permanently inactive"
                        : !grant.identity_enabled
                          ? "Suspended · identity disabled"
                          : grant.group_enabled === false
                            ? "Suspended · group disabled"
                            : grant.effective
                              ? "Effective now"
                              : "Inactive";
                  },
                },
              ]
            : []),
        ]}
        pagination={page.pagination}
        empty={
          retained
            ? "No retained assignments."
            : "No effective delegations. Operator authority is shown separately."
        }
      />
    </SpaceBetween>
  );
}

function Tokens({
  path,
  currentToken,
  notify,
  endSession,
}: {
  path: string;
  currentToken: string;
  notify: (message: string) => void;
  endSession: () => void;
}) {
  const page = usePage<Token>(path);
  const [creating, setCreating] = useState(false),
    [revoking, setRevoking] = useState<Token | null>(null),
    [secret, setSecret] = useState(""),
    [copied, setCopied] = useState(false),
    [copyError, setCopyError] = useState("");
  return (
    <SpaceBetween size="m">
      <Failure error={page.error} onRetry={page.reload} />
      <Table
        wrapLines
        trackBy="id"
        items={page.items}
        loading={page.loading}
        loadingText="Loading API tokens"
        header={
          <Header
            actions={
              <Button variant="primary" onClick={() => setCreating(true)}>
                Create token
              </Button>
            }
          >
            API tokens
          </Header>
        }
        columnDefinitions={[
          {
            id: "label",
            header: "Label",
            cell: (item) => (
              <>
                {item.label}
                {item.id === currentToken && (
                  <div>
                    <strong>Current sign-in</strong>
                  </div>
                )}
              </>
            ),
          },
          {
            id: "status",
            header: "State",
            cell: (item) =>
              item.status || (item.revoked_at ? "revoked" : "active"),
          },
          {
            id: "expiry",
            header: "Expiry",
            cell: (item) =>
              item.expires_at
                ? new Date(item.expires_at).toLocaleString()
                : "No expiry",
          },
          {
            id: "created",
            header: "Created",
            cell: (item) => new Date(item.created_at).toLocaleString(),
          },
          { id: "id", header: "Token ID", cell: (item) => item.id },
          {
            id: "actions",
            header: "Actions",
            cell: (item) => (
              <Button
                ariaLabel={"Revoke " + item.label}
                disabled={!!item.revoked_at || item.status === "revoked"}
                onClick={() => setRevoking(item)}
              >
                Revoke
              </Button>
            ),
          },
        ]}
        pagination={page.pagination}
        empty="No API tokens."
      />
      {creating && (
        <TokenForm
          path={path}
          close={() => setCreating(false)}
          done={(value) => {
            setCreating(false);
            setSecret(value);
            setCopied(false);
            setCopyError("");
            page.reload();
          }}
        />
      )}
      {!!secret && (
        <Modal
          visible
          header="Copy your new API token"
          closeAriaLabel="Close new token"
          onDismiss={() => setSecret("")}
          footer={
            <Button variant="primary" onClick={() => setSecret("")}>
              Done
            </Button>
          }
        >
          <SpaceBetween size="m">
            <Alert type="warning">
              This secret is shown only once. Store it securely before closing.
              It cannot be recovered from token metadata.
            </Alert>
            <code className="dns-value">{secret}</code>
            <Button
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(secret);
                  setCopied(true);
                  setCopyError("");
                } catch {
                  setCopyError(
                    "Clipboard access was unavailable. Select and copy the token text before closing.",
                  );
                }
              }}
            >
              Copy token
            </Button>
            {copied && <span role="status">Token copied.</span>}
            <Failure error={copyError} />
          </SpaceBetween>
        </Modal>
      )}
      {revoking && (
        <ConfirmAction
          title="Revoke token"
          close={() => setRevoking(null)}
          action={async () => {
            await request(path + "/" + encodeURIComponent(revoking.id), {
              method: "DELETE",
            });
          }}
          done={() => {
            const current = revoking.id === currentToken;
            setRevoking(null);
            if (current) endSession();
            else {
              notify("API token revoked.");
              page.reload();
            }
          }}
        >
          Revoke {revoking.label}? Clients using this token will lose access.{" "}
          {revoking.id === currentToken
            ? "This token backs your current sign-in. Its browser sessions will end and you will return to sign-in."
            : "Other tokens are unaffected."}
        </ConfirmAction>
      )}
    </SpaceBetween>
  );
}

function TokenForm({
  path,
  close,
  done,
}: {
  path: string;
  close: () => void;
  done: (secret: string) => void;
}) {
  const [label, setLabel] = useState(""),
    [expiry, setExpiry] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function save() {
    if (busy) return;
    if (!/^[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?$/.test(label)) {
      setError(
        "Use a lowercase label of 1–63 letters, digits, dots, underscores or hyphens, starting and ending with a letter or digit.",
      );
      return;
    }
    const time = expiry ? new Date(expiry) : null;
    if (
      time &&
      (!Number.isFinite(time.valueOf()) ||
        time <= new Date() ||
        !/(Z|[+-]\d\d:\d\d)$/.test(expiry))
    ) {
      setError(
        "Enter a future ISO 8601 timestamp with an explicit timezone, for example 2027-01-31T12:00:00Z.",
      );
      return;
    }
    setBusy(true);
    setError("");
    try {
      const result = await request<Token & { secret: string }>(path, {
        method: "POST",
        body: JSON.stringify({
          label,
          ...(time ? { expires_at: time.toISOString() } : {}),
        }),
      });
      if (!result?.secret)
        throw new Error(
          "Token creation succeeded, but the secret could not be read. Close this form, inspect token metadata and revoke the unusable token before creating another. This request was not retried.",
        );
      done(result.secret);
    } catch (error) {
      setError(managementError(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      visible
      header="Create token"
      closeAriaLabel="Close token creation"
      onDismiss={() => {
        if (!busy) close();
      }}
      footer={
        <SpaceBetween direction="horizontal" size="xs">
          <Button disabled={busy} onClick={close}>
            Cancel
          </Button>
          <Button variant="primary" loading={busy} onClick={save}>
            Create token
          </Button>
        </SpaceBetween>
      }
    >
      <SpaceBetween size="m">
        <Failure error={error} />
        <Field label="Label" value={label} onChange={setLabel} />
        <Field
          label="Expiry (optional)"
          value={expiry}
          onChange={setExpiry}
          description="ISO 8601 with timezone, such as 2027-01-31T12:00:00Z. Leave blank for no expiry."
        />
      </SpaceBetween>
    </Modal>
  );
}

function GroupDetails({ id, notify }: ManagementProps & { id: string }) {
  const [group, setGroup] = useState<Group | null>(null),
    [error, setError] = useState(""),
    [editing, setEditing] = useState(false),
    [toggle, setToggle] = useState(false),
    [adding, setAdding] = useState(false),
    [removing, setRemoving] = useState<Identity | null>(null),
    [selected, setSelected] = useState(""),
    [prefix, setPrefix] = useState(""),
    [filter, setFilter] = useState("");
  const path = managementPath("groups", id),
    page = usePage<Identity>(
      queryPath(path + "/members", { handle_prefix: filter }),
    );
  async function load() {
    try {
      setGroup(await request<Group>(path));
      setError("");
    } catch (error) {
      setError(managementError(error));
    }
  }
  useEffect(() => {
    void load();
  }, [id]);
  useDetailHeading(group?.handle);
  if (!group)
    return (
      <SpaceBetween size="m">
        <Failure error={error} />
        <Button onClick={load}>Reload group</Button>
        {!error && <Spinner />}
      </SpaceBetween>
    );
  return (
    <SpaceBetween size="l">
      <Header
        variant="h1"
        description="Group details"
        actions={
          <SpaceBetween direction="horizontal" size="xs">
            <Button onClick={() => setEditing(true)}>Edit group</Button>
            <Button onClick={() => setToggle(true)}>
              {group.enabled ? "Disable group" : "Enable group"}
            </Button>
          </SpaceBetween>
        }
      >
        {group.handle}
      </Header>
      <Failure error={error} />
      <Container>
        <dl className="details">
          <dt>Display name</dt>
          <dd>{group.display_name || "—"}</dd>
          <dt>State</dt>
          <dd>
            {group.enabled
              ? "Enabled"
              : "Disabled · retained memberships and delegations contribute no effective authority"}
          </dd>
          <dt>Group ID</dt>
          <dd>{group.id}</dd>
        </dl>
      </Container>
      <Failure error={page.error} onRetry={page.reload} />
      <Table
        wrapLines
        trackBy="id"
        items={page.items}
        loading={page.loading}
        loadingText="Loading group members"
        header={
          <Header
            actions={
              <Button
                variant="primary"
                onClick={() => {
                  setSelected("");
                  setAdding(true);
                }}
              >
                Add member
              </Button>
            }
          >
            Direct members
          </Header>
        }
        filter={
          <div className="filter-grid">
            <Field
              label="Member handle prefix"
              value={prefix}
              onChange={setPrefix}
            />
            <Button
              onClick={() => {
                setFilter(prefix);
                page.reload();
              }}
            >
              Search members
            </Button>
          </div>
        }
        columnDefinitions={[
          {
            id: "handle",
            header: "Identity",
            cell: (item) => (
              <Link href={"#/identities/" + item.id}>{item.handle}</Link>
            ),
          },
          {
            id: "kind",
            header: "Kind",
            cell: (item) =>
              item.kind === "service" ? "Service identity" : "User",
          },
          {
            id: "state",
            header: "State",
            cell: (item) => (item.enabled ? "Enabled" : "Disabled"),
          },
          {
            id: "remove",
            header: "Actions",
            cell: (item) => (
              <Button
                ariaLabel={"Remove " + item.handle}
                onClick={() => setRemoving(item)}
              >
                Remove member
              </Button>
            ),
          },
        ]}
        pagination={page.pagination}
        empty="No direct members match."
      />
      {editing && (
        <ResourceForm
          kind="groups"
          resource={group}
          close={() => setEditing(false)}
          done={(item) => {
            setGroup(item);
            setEditing(false);
            notify("Group profile saved.");
          }}
        />
      )}
      {toggle && (
        <ConfirmAction
          title={group.enabled ? "Disable group" : "Enable group"}
          close={() => setToggle(false)}
          action={async () => {
            await request(path, {
              method: "PATCH",
              body: JSON.stringify({ enabled: !group.enabled }),
            });
          }}
          done={() => {
            setToggle(false);
            notify("Group state updated.");
            void load();
          }}
        >
          {group.enabled
            ? "Disable this group? Memberships and delegations remain, but stop contributing effective authority."
            : "Re-enable this group? This can restore eligible access for its enabled members through retained delegations. Revoked grants and retired zone lifetimes are not restored."}
        </ConfirmAction>
      )}
      {adding && (
        <AddMember
          path={path}
          selected={selected}
          setSelected={setSelected}
          close={() => setAdding(false)}
          done={() => {
            setAdding(false);
            notify("Group member added.");
            page.reload();
          }}
        />
      )}
      {removing && (
        <ConfirmAction
          title="Remove member"
          close={() => setRemoving(null)}
          action={async () => {
            await request(
              path + "/members/" + encodeURIComponent(removing.id),
              { method: "DELETE" },
            );
          }}
          done={() => {
            setRemoving(null);
            notify("Group member removed.");
            page.reload();
          }}
        >
          Remove {removing.handle} from this group? Its access through this
          group will end at subsequent authorization decisions. Other
          assignments remain.
        </ConfirmAction>
      )}
    </SpaceBetween>
  );
}
function AddMember({
  path,
  selected,
  setSelected,
  close,
  done,
}: {
  path: string;
  selected: string;
  setSelected: (id: string) => void;
  close: () => void;
  done: () => void;
}) {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  return (
    <Modal
      visible
      size="large"
      header="Add member"
      closeAriaLabel="Close add member"
      onDismiss={() => {
        if (!busy) close();
      }}
      footer={
        <SpaceBetween direction="horizontal" size="xs">
          <Button disabled={busy} onClick={close}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            onClick={async () => {
              if (busy) return;
              if (!selected) {
                setError("Select an identity.");
                return;
              }
              setBusy(true);
              try {
                await request(
                  path + "/members/" + encodeURIComponent(selected),
                  { method: "PUT" },
                );
                done();
              } catch (error) {
                setError(managementError(error));
              } finally {
                setBusy(false);
              }
            }}
          >
            Add member
          </Button>
        </SpaceBetween>
      }
    >
      <SpaceBetween size="m">
        <Failure error={error} />
        <RemoteSelector
          kind="identities"
          label="Identity"
          value={selected}
          onChange={setSelected}
        />
        <Alert type="info">
          Membership is retained even when the identity or group is disabled.
          Re-enabling both can restore eligible group access.
        </Alert>
      </SpaceBetween>
    </Modal>
  );
}
