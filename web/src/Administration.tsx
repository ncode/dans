import { useEffect, useState } from "react";
import Alert from "@cloudscape-design/components/alert";
import Button from "@cloudscape-design/components/button";
import Header from "@cloudscape-design/components/header";
import Modal from "@cloudscape-design/components/modal";
import SpaceBetween from "@cloudscape-design/components/space-between";
import StatusIndicator from "@cloudscape-design/components/status-indicator";
import Table from "@cloudscape-design/components/table";
import { request } from "./api.ts";
import {
  allPages,
  Choice,
  Failure,
  Field,
  Pager,
  messageOf,
} from "./controls.tsx";
import type { Delegation, Identity, Page, Zone } from "./domain.ts";
interface Audit {
  id: string;
  occurred_at: string;
  actor_id: string | null;
  action: string;
  target_type: string;
  target_id: string | null;
  result: string;
  request_id: string;
  details: Record<string, unknown>;
}
interface Group {
  id: string;
  handle: string;
  display_name: string | null;
  enabled: boolean;
}
interface Binding {
  id: string;
  zone_id: string;
  zone_name: string;
  status: string;
}
export function Administration({
  section,
  notify,
}: {
  section: string;
  notify: (message: string) => void;
}) {
  const [delegations, setDelegations] = useState<Delegation[]>([]),
    [audits, setAudits] = useState<Audit[]>([]),
    [next, setNext] = useState<string | null>(null),
    [cursor, setCursor] = useState(""),
    [history, setHistory] = useState<string[]>([]),
    [loading, setLoading] = useState(true),
    [error, setError] = useState(""),
    [create, setCreate] = useState(false),
    [detail, setDetail] = useState<Delegation | Audit | null>(null),
    [revoke, setRevoke] = useState<Delegation | null>(null),
    [busy, setBusy] = useState(false),
    [result, setResult] = useState(""),
    [action, setAction] = useState(""),
    [filters, setFilters] = useState({ result: "", action: "" });
  const audit = section === "Audit";
  async function load(signal?: AbortSignal) {
    setLoading(true);
    try {
      const query = new URLSearchParams({
        limit: "100",
        ...(cursor ? { cursor } : {}),
        ...(audit
          ? Object.fromEntries(
              Object.entries(filters).filter(([, value]) => value),
            )
          : {}),
      });
      if (audit) {
        const page = await request<Page<Audit>>("/dans/audit-events?" + query, {
          signal,
        });
        setAudits(page.items);
        setNext(page.next_cursor);
      } else {
        const page = await request<Page<Delegation>>(
          "/dans/delegations?" + query,
          { signal },
        );
        setDelegations(page.items);
        setNext(page.next_cursor);
      }
      setError("");
    } catch (error) {
      if (!signal?.aborted) setError(messageOf(error));
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }
  useEffect(() => {
    setCursor("");
    setHistory([]);
    setDetail(null);
  }, [section]);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [section, cursor, filters]);
  async function revokeGrant() {
    if (!revoke || busy) return;
    setBusy(true);
    try {
      await request("/dans/delegations/" + encodeURIComponent(revoke.id), {
        method: "DELETE",
      });
      setRevoke(null);
      notify("Delegation revoked.");
      await load();
    } catch (error) {
      setError(messageOf(error));
    } finally {
      setBusy(false);
    }
  }
  const pager = (
    <Pager
      previous={!!history.length}
      next={!!next}
      busy={loading}
      onPrevious={() => {
        setCursor(history[history.length - 1]);
        setHistory(history.slice(0, -1));
      }}
      onNext={() => {
        if (next) {
          setHistory([...history, cursor]);
          setCursor(next);
        }
      }}
    />
  );
  return (
    <SpaceBetween size="l">
      <Failure error={error} />
      {audit ? (
        <Table
          variant="full-page"
          wrapLines
          trackBy="id"
          items={audits}
          loading={loading}
          loadingText="Loading audit history"
          header={
            <Header
              variant="h1"
              description="Observed outcomes of DNS and access-management operations."
              actions={<Button onClick={() => load()}>Reload audit</Button>}
            >
              Audit
            </Header>
          }
          filter={
            <div className="filter-grid">
              <Field label="Action" value={action} onChange={setAction} />
              <Choice
                label="Result"
                value={result}
                onChange={setResult}
                options={[
                  "",
                  "pending",
                  "succeeded",
                  "failed",
                  "unknown",
                  "denied",
                ].map((value) => ({ value, label: value || "All results" }))}
              />
              <Button
                onClick={() => {
                  setCursor("");
                  setHistory([]);
                  setFilters({ result, action });
                }}
              >
                Filter audit
              </Button>
            </div>
          }
          columnDefinitions={[
            {
              id: "time",
              header: "Time",
              cell: (item) => new Date(item.occurred_at).toLocaleString(),
            },
            { id: "action", header: "Action", cell: (item) => item.action },
            {
              id: "result",
              header: "Result",
              cell: (item) => (
                <StatusIndicator
                  type={
                    item.result === "succeeded"
                      ? "success"
                      : item.result === "pending"
                        ? "in-progress"
                        : item.result === "unknown"
                          ? "warning"
                          : "error"
                  }
                >
                  {item.result}
                </StatusIndicator>
              ),
            },
            {
              id: "target",
              header: "Target",
              cell: (item) => (
                <span className="dns-value">{item.target_id ?? "—"}</span>
              ),
            },
            {
              id: "details",
              header: "Details",
              cell: (item) => (
                <Button
                  onClick={() => setDetail(item)}
                  ariaLabel={"Inspect audit " + item.id}
                >
                  Inspect
                </Button>
              ),
            },
          ]}
          pagination={pager}
          empty={<span>No audit events match these filters.</span>}
        />
      ) : (
        <Table
          variant="full-page"
          trackBy="id"
          items={delegations}
          loading={loading}
          loadingText="Loading delegations"
          wrapLines
          header={
            <Header
              variant="h1"
              description="Allow selected identities or groups to change complete RRsets."
              actions={
                <Button variant="primary" onClick={() => setCreate(true)}>
                  Create delegation
                </Button>
              }
            >
              Delegations
            </Header>
          }
          columnDefinitions={[
            {
              id: "selectors",
              header: "Name selectors",
              cell: (item) =>
                item.selectors.map((selector) => (
                  <div
                    key={selector.kind + selector.value}
                    className="dns-value"
                  >
                    {selector.kind}: {selector.value}
                  </div>
                )),
            },
            {
              id: "type",
              header: "Record types",
              cell: (item) => item.record_types?.join(", ") || "All",
            },
            {
              id: "actions",
              header: "Change kinds",
              cell: (item) => item.change_kinds?.join(", ") || "All",
            },
            {
              id: "status",
              header: "Status",
              cell: (item) => (item.revoked_at ? "Revoked" : "Active"),
            },
            {
              id: "details",
              header: "Actions",
              cell: (item) => (
                <SpaceBetween direction="horizontal" size="xs">
                  <Button onClick={() => setDetail(item)}>Inspect</Button>
                  <Button
                    disabled={!!item.revoked_at}
                    onClick={() => setRevoke(item)}
                  >
                    Revoke
                  </Button>
                </SpaceBetween>
              ),
            },
          ]}
          pagination={pager}
          empty={<span>No delegations have been created.</span>}
        />
      )}
      {create && (
        <DelegationForm
          close={() => setCreate(false)}
          done={() => {
            setCreate(false);
            notify("Delegation created.");
            void load();
          }}
        />
      )}
      <Modal
        visible={!!detail}
        header={audit ? "Audit event" : "Delegation details"}
        closeAriaLabel="Close details"
        onDismiss={() => setDetail(null)}
        footer={<Button onClick={() => setDetail(null)}>Close</Button>}
      >
        <SpaceBetween size="m">
          {audit && (
            <Alert type="info">
              Unknown means the mutation may have completed. Inspect current DNS
              state before making another change. Audit history never replays a
              write.
            </Alert>
          )}
          <dl className="details">
            {detail &&
              Object.entries(detail).map(([key, value]) => (
                <div key={key}>
                  <dt>{key.replaceAll("_", " ")}</dt>
                  <dd>
                    {typeof value === "object"
                      ? JSON.stringify(value, null, 2)
                      : String(value ?? "—")}
                  </dd>
                </div>
              ))}
          </dl>
        </SpaceBetween>
      </Modal>
      <Modal
        visible={!!revoke}
        header="Revoke delegation"
        closeAriaLabel="Close revocation"
        onDismiss={() => {
          if (!busy) setRevoke(null);
        }}
        footer={
          <SpaceBetween direction="horizontal" size="xs">
            <Button disabled={busy} onClick={() => setRevoke(null)}>
              Cancel
            </Button>
            <Button variant="primary" loading={busy} onClick={revokeGrant}>
              Revoke delegation
            </Button>
          </SpaceBetween>
        }
      >
        <SpaceBetween size="m">
          <Alert type="warning">
            Revoke this delegation? Requests already authorized may finish;
            subsequent requests lose this grant.
          </Alert>
          <p>{revoke?.selectors.map((value) => value.value).join(", ")}</p>
          <Failure error={error} />
        </SpaceBetween>
      </Modal>
    </SpaceBetween>
  );
}
function DelegationForm({
  close,
  done,
}: {
  close: () => void;
  done: () => void;
}) {
  const [zones, setZones] = useState<Zone[]>([]),
    [bindings, setBindings] = useState<Binding[]>([]),
    [people, setPeople] = useState<{ value: string; label: string }[]>([]),
    [zone, setZone] = useState(""),
    [grantee, setGrantee] = useState(""),
    [selectors, setSelectors] = useState<Delegation["selectors"]>([
      { kind: "exact", value: "" },
    ]),
    [types, setTypes] = useState(""),
    [kinds, setKinds] = useState(""),
    [loading, setLoading] = useState(true),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  useEffect(() => {
    Promise.all([
      request<Zone[]>("/servers/localhost/zones"),
      allPages<Binding>("/dans/zone-bindings?status=active"),
      allPages<Identity>("/dans/identities?enabled=true"),
      allPages<Group>("/dans/groups?enabled=true"),
    ])
      .then(([zones, bindings, identities, groups]) => {
        setZones(zones);
        setBindings(bindings);
        setPeople([
          ...identities.map((item) => ({
            value: "identity:" + item.id,
            label:
              (item.display_name || item.handle) + " (" + item.handle + ")",
          })),
          ...groups.map((item) => ({
            value: "group:" + item.id,
            label:
              "Group: " +
              (item.display_name || item.handle) +
              " (" +
              item.handle +
              ")",
          })),
        ]);
      })
      .catch((error) => setError(messageOf(error)))
      .finally(() => setLoading(false));
  }, []);
  async function save() {
    if (busy || loading) return;
    if (!zone || !grantee || selectors.some((item) => !item.value)) {
      setError("Select a zone and grantee and complete every name selector.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      let binding = bindings.find((item) => item.zone_id === zone);
      if (!binding) {
        await request("/dans/zone-bindings", {
          method: "POST",
          body: JSON.stringify({ zone_id: zone }),
        });
        binding = (
          await allPages<Binding>("/dans/zone-bindings?status=active")
        ).find((item) => item.zone_id === zone);
        if (!binding)
          throw new Error(
            "Zone binding could not be confirmed. Reload before creating the delegation.",
          );
        setBindings([...bindings, binding]);
      }
      const [kind, id] = grantee.split(":");
      const list = (value: string) =>
        value.trim()
          ? value
              .split(",")
              .map((item) => item.trim().toUpperCase())
              .filter(Boolean)
          : undefined;
      await request("/dans/delegations", {
        method: "POST",
        body: JSON.stringify({
          zone_binding_id: binding.id,
          [kind + "_id"]: id,
          selectors,
          record_types: list(types),
          change_kinds: list(kinds),
        }),
      });
      done();
    } catch (error) {
      setError(messageOf(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      visible
      size="large"
      header="Create delegation"
      closeAriaLabel="Close delegation form"
      onDismiss={() => {
        if (!busy) close();
      }}
      footer={
        <SpaceBetween direction="horizontal" size="xs">
          <Button disabled={busy} onClick={close}>
            Cancel
          </Button>
          <Button variant="primary" loading={busy || loading} onClick={save}>
            Create delegation
          </Button>
        </SpaceBetween>
      }
    >
      <SpaceBetween size="m">
        <Failure error={error} />
        <Choice
          label="Zone"
          value={zone}
          onChange={setZone}
          options={zones.map((item) => ({ value: item.id, label: item.name }))}
        />
        <Choice
          label="Grantee"
          value={grantee}
          onChange={setGrantee}
          options={people}
        />
        {selectors.map((selector, index) => (
          <div key={index} className="row-fields">
            <Field
              label={"Name pattern " + (index + 1)}
              value={selector.value}
              onChange={(value) =>
                setSelectors(
                  selectors.map((item, i) =>
                    i === index ? { ...item, value } : item,
                  ),
                )
              }
            />
            <Choice
              label={"Selector kind " + (index + 1)}
              value={selector.kind}
              onChange={(value) =>
                setSelectors(
                  selectors.map((item, i) =>
                    i === index
                      ? { ...item, kind: value === "exact" ? "exact" : "glob" }
                      : item,
                  ),
                )
              }
              options={[
                { label: "Exact name", value: "exact" },
                { label: "Glob pattern", value: "glob" },
              ]}
            />
            <Button
              disabled={selectors.length === 1}
              onClick={() =>
                setSelectors(selectors.filter((_, i) => i !== index))
              }
            >
              Remove selector {index + 1}
            </Button>
          </div>
        ))}
        <Button
          disabled={selectors.length >= 100}
          onClick={() =>
            setSelectors([...selectors, { kind: "exact", value: "" }])
          }
        >
          Add selector
        </Button>
        <Field
          label="Record type restrictions"
          value={types}
          onChange={setTypes}
          description="Comma-separated types; leave blank for all types."
        />
        <Field
          label="Change restrictions"
          value={kinds}
          onChange={setKinds}
          description="REPLACE, DELETE, EXTEND, PRUNE; leave blank for all changes."
        />
        <Alert type="info">
          Use exact selectors for the zone apex and literal wildcard owners. A
          glob * may span DNS labels.
        </Alert>
      </SpaceBetween>
    </Modal>
  );
}
