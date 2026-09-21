import { useEffect, useRef, useState } from "react";
import Alert from "@cloudscape-design/components/alert";
import Button from "@cloudscape-design/components/button";
import Checkbox from "@cloudscape-design/components/checkbox";
import Container from "@cloudscape-design/components/container";
import ExpandableSection from "@cloudscape-design/components/expandable-section";
import Header from "@cloudscape-design/components/header";
import Link from "@cloudscape-design/components/link";
import Modal from "@cloudscape-design/components/modal";
import SpaceBetween from "@cloudscape-design/components/space-between";
import StatusIndicator from "@cloudscape-design/components/status-indicator";
import Table from "@cloudscape-design/components/table";
import TextFilter from "@cloudscape-design/components/text-filter";
import { APIError, browsePath, request, zonePath } from "./api.ts";
import { Choice, Failure, Field, Pager, messageOf } from "./controls.tsx";
import {
  canChange,
  type Zone,
  type Identity,
  type Delegation,
  type RRset,
  type BrowsePage,
} from "./domain.ts";
import { liveRRset, RecordEditor } from "./RecordEditor.tsx";
export function Zones({
  route,
  identity,
  grants,
  grantsComplete,
  notify,
}: {
  route: string;
  identity: Identity;
  grants: Delegation[];
  grantsComplete: boolean;
  notify: (message: string) => void;
}) {
  const [zones, setZones] = useState<Zone[]>([]),
    [filter, setFilter] = useState(""),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true),
    [form, setForm] = useState<Zone | null | undefined>(),
    [deleting, setDeleting] = useState<Zone | null>(null),
    [busy, setBusy] = useState(false);
  let selected = "";
  try {
    selected = decodeURIComponent(route.split("/").slice(2).join("/"));
  } catch {
    /* A malformed console URL returns to the zone list. */
  }
  const zone = zones.find((item) => item.id === selected);
  async function reload() {
    setLoading(true);
    try {
      setZones(await request<Zone[]>("/servers/localhost/zones"));
      setError("");
    } catch (error) {
      setError(messageOf(error));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void reload();
  }, []);
  async function editZone(item: Zone) {
    setBusy(true);
    try {
      setForm(await request<Zone>(zonePath(item.id) + "?rrsets=false"));
    } catch (error) {
      setError(messageOf(error));
    } finally {
      setBusy(false);
    }
  }
  async function removeZone() {
    if (!deleting || busy) return;
    setBusy(true);
    setError("");
    try {
      await request(zonePath(deleting.id), { method: "DELETE" });
      setDeleting(null);
      notify("Zone deleted.");
      location.hash = "#/zones";
      await reload();
    } catch (error) {
      setError(messageOf(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <SpaceBetween size="l">
      <Failure error={error} />
      {zone ? (
        <>
          <Header
            variant="h1"
            description={"Zone · " + zone.kind}
            actions={
              identity.operator ? (
                <SpaceBetween direction="horizontal" size="xs">
                  <Button onClick={() => editZone(zone)} loading={busy}>
                    Edit zone
                  </Button>
                  <Button
                    onClick={() => {
                      setError("");
                      setDeleting(zone);
                    }}
                  >
                    Delete zone
                  </Button>
                </SpaceBetween>
              ) : undefined
            }
          >
            {zone.name}
          </Header>
          <ZoneRecords
            key={zone.id}
            zone={zone}
            identity={identity}
            grants={grants}
            grantsComplete={grantsComplete}
            notify={notify}
          />
        </>
      ) : (
        <Table
          trackBy="id"
          items={zones.filter((item) =>
            item.name.toLowerCase().includes(filter.toLowerCase()),
          )}
          loading={loading}
          loadingText="Loading zones"
          variant="full-page"
          columnDefinitions={[
            {
              id: "name",
              header: "Zone name",
              cell: (item) => (
                <Link href={"#/zones/" + encodeURIComponent(item.id)}>
                  {item.name}
                </Link>
              ),
            },
            { id: "kind", header: "Kind", cell: (item) => item.kind },
            {
              id: "dnssec",
              header: "DNSSEC",
              cell: (item) => (item.dnssec ? "Signed" : "Unsigned"),
            },
            {
              id: "serial",
              header: "Serial",
              cell: (item) => item.serial ?? "—",
            },
          ]}
          header={
            <Header
              variant="h1"
              counter={"(" + zones.length + ")"}
              description="Browse DNS data and manage complete RRsets."
              actions={
                identity.operator ? (
                  <Button variant="primary" onClick={() => setForm(null)}>
                    Create zone
                  </Button>
                ) : (
                  <Button onClick={reload}>Reload zones</Button>
                )
              }
            >
              Zones
            </Header>
          }
          filter={
            <TextFilter
              filteringText={filter}
              filteringAriaLabel="Filter zones"
              filteringPlaceholder="Find zones by name"
              onChange={({ detail }) => setFilter(detail.filteringText)}
            />
          }
          empty={
            <SpaceBetween size="s">
              <strong>No zones found</strong>
              <span>
                {identity.operator
                  ? "Create a zone to get started."
                  : "No readable zones are available."}
              </span>
            </SpaceBetween>
          }
        />
      )}
      {form !== undefined && (
        <ZoneForm
          initial={form}
          close={() => setForm(undefined)}
          done={async () => {
            setForm(undefined);
            notify(form ? "Zone saved." : "Zone created.");
            await reload();
          }}
        />
      )}
      <Modal
        visible={!!deleting}
        header="Delete zone"
        closeAriaLabel="Close zone deletion"
        onDismiss={() => {
          if (!busy) setDeleting(null);
        }}
        footer={
          <SpaceBetween direction="horizontal" size="xs">
            <Button onClick={() => setDeleting(null)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" loading={busy} onClick={removeZone}>
              Delete zone
            </Button>
          </SpaceBetween>
        }
      >
        <SpaceBetween size="m">
          <Alert type="warning">
            Delete {deleting?.name} and every RRset? Existing delegations will
            be revoked before deletion and remain revoked if deletion fails.
          </Alert>
          <Failure error={error} />
        </SpaceBetween>
      </Modal>
    </SpaceBetween>
  );
}
function ZoneRecords({
  zone,
  identity,
  grants,
  grantsComplete,
  notify,
}: {
  zone: Zone;
  identity: Identity;
  grants: Delegation[];
  grantsComplete: boolean;
  notify: (message: string) => void;
}) {
  const [page, setPage] = useState<BrowsePage | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [cursor, setCursor] = useState(""),
    [history, setHistory] = useState<string[]>([]),
    [name, setName] = useState(""),
    [match, setMatch] = useState("prefix"),
    [type, setType] = useState(""),
    [filters, setFilters] = useState({ name: "", match: "prefix", type: "" }),
    [editor, setEditor] = useState<{
      initial: RRset | null;
      deleting: boolean;
    } | null>(null);
  const path = browsePath(zone.id);
  const readSequence = useRef(0);
  const [fetching, setFetching] = useState(false);
  async function load(signal?: AbortSignal) {
    const sequence = ++readSequence.current;
    setFetching(true);
    try {
      const query = new URLSearchParams({
        ...filters,
        ...(cursor ? { cursor } : {}),
      });
      const result = await request<BrowsePage>(path + "?" + query, { signal });
      if (sequence !== readSequence.current || signal?.aborted) return;
      setPage(result);
      setError("");
    } catch (error) {
      if (signal?.aborted || sequence !== readSequence.current) return;
      setPage((previous) =>
        previous ? { ...previous, state: "stale" } : null,
      );
      if (error instanceof APIError && error.status === 409) {
        setCursor("");
        setHistory([]);
        setError("The browsing snapshot changed. Returning to the first page.");
      } else setError(messageOf(error));
    } finally {
      if (sequence === readSequence.current && !signal?.aborted)
        setFetching(false);
    }
  }
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    const timer = setInterval(() => {
      void load(controller.signal);
    }, 5000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [cursor, filters]);
  async function refresh() {
    if (busy) return;
    setBusy(true);
    try {
      await request(path + "/refresh", { method: "POST" });
      setCursor("");
      setHistory([]);
      if (!cursor) await load();
    } catch (error) {
      setPage((previous) =>
        previous ? { ...previous, state: "stale" } : null,
      );
      setError(messageOf(error));
    } finally {
      setBusy(false);
    }
  }
  async function open(item: RRset, deleting = false) {
    if (busy) return;
    setBusy(true);
    try {
      const current = await liveRRset(zone, item.name, item.type);
      if (!current)
        throw new Error("This RRset no longer exists. Refresh the table.");
      setEditor({ initial: current, deleting });
      setError("");
    } catch (error) {
      setError(messageOf(error));
    } finally {
      setBusy(false);
    }
  }
  const allowed = (item: RRset, action: string) =>
    !grantsComplete ||
    canChange(identity.operator, grants, zone, item.name, item.type, action);
  const zoneGrants = grants.filter((grant) => grant.zone_id === zone.id);
  return (
    <SpaceBetween size="m">
      <Container>
        <SpaceBetween size="m">
          <div className="filter-grid">
            <Field label="Owner name" value={name} onChange={setName} />
            <Choice
              label="Match"
              value={match}
              onChange={setMatch}
              options={[
                { value: "prefix", label: "Starts with" },
                { value: "exact", label: "Exact name" },
              ]}
            />
            <Field
              label="Record type"
              value={type}
              onChange={(value) => setType(value.toUpperCase())}
            />
            <Button
              onClick={() => {
                setHistory([]);
                setCursor("");
                setFilters({ name, match, type });
              }}
            >
              Search
            </Button>
          </div>
          <div role="status">
            <StatusIndicator
              type={
                page?.state === "ready"
                  ? "success"
                  : page?.state === "stale"
                    ? "warning"
                    : "in-progress"
              }
            >
              {page?.state === "ready"
                ? "Up to date"
                : page?.state === "stale"
                  ? "Stale browsing data"
                  : page?.state === "refreshing"
                    ? "Refreshing browsing data"
                    : "Indexing zone"}
            </StatusIndicator>
            {page?.last_refreshed_at && (
              <span>
                {" "}
                · Last full refresh{" "}
                {new Date(page.last_refreshed_at).toLocaleString()}
              </span>
            )}
          </div>
          {page?.error && (
            <Alert type="warning">
              {page.error}{" "}
              {page.last_refreshed_at
                ? "Previous complete results remain visible."
                : "No complete results are available yet."}{" "}
              Try Refresh zone.
            </Alert>
          )}
          <Failure error={error} />
        </SpaceBetween>
      </Container>
      <div className="rrset-table">
        <Table
          trackBy={(item) => item.name + " " + item.type}
          items={page?.items ?? []}
          loading={!page || page.state === "indexing"}
          loadingText="Building the zone index. Large zones may take a while."
          wrapLines
          columnDefinitions={[
            {
              id: "name",
              header: "Owner name",
              cell: (item) => <span className="dns-value">{item.name}</span>,
              minWidth: 200,
            },
            {
              id: "type",
              header: "Type",
              cell: (item) => item.type,
              minWidth: 80,
            },
            {
              id: "ttl",
              header: "TTL",
              cell: (item) => item.ttl,
              minWidth: 80,
            },
            {
              id: "values",
              header: "Values",
              minWidth: 320,
              width: 440,
              cell: (item) => (
                <div
                  className="values rrset-values"
                  tabIndex={0}
                  role="region"
                  aria-label={
                    "Complete values for " + item.name + " " + item.type
                  }
                >
                  {item.records.map((value, index) => (
                    <span className="dns-value" key={index}>
                      {value.content}
                      {value.disabled ? " (disabled)" : ""}
                    </span>
                  ))}
                  {!!item.comments?.length && (
                    <ExpandableSection
                      headerText={item.comments.length + " comments"}
                    >
                      {item.comments.map((value, index) => (
                        <p className="dns-value" key={index}>
                          {value.content}{" "}
                          {value.account ? "— " + value.account : ""}
                        </p>
                      ))}
                    </ExpandableSection>
                  )}
                </div>
              ),
            },
            {
              id: "actions",
              header: "Actions",
              minWidth: 180,
              cell: (item) => (
                <SpaceBetween direction="horizontal" size="xs">
                  <Button
                    disabled={busy || !allowed(item, "REPLACE")}
                    ariaLabel={"Edit " + item.name + " " + item.type}
                    onClick={() => open(item)}
                  >
                    Edit
                  </Button>
                  <Button
                    disabled={busy || !allowed(item, "DELETE")}
                    ariaLabel={"Delete " + item.name + " " + item.type}
                    onClick={() => open(item, true)}
                  >
                    Delete
                  </Button>
                </SpaceBetween>
              ),
            },
          ]}
          header={
            <Header
              variant="h2"
              description="One row contains every value in an RRset. Pages contain up to 100 RRsets. Scroll horizontally to see all columns."
              actions={
                <SpaceBetween direction="horizontal" size="xs">
                  <Button loading={busy} onClick={refresh}>
                    Refresh zone
                  </Button>
                  <Button
                    variant="primary"
                    disabled={!identity.operator && !zoneGrants.length}
                    onClick={() =>
                      setEditor({ initial: null, deleting: false })
                    }
                  >
                    Create RRset
                  </Button>
                </SpaceBetween>
              }
            >
              RRsets
            </Header>
          }
          empty={<span>No RRsets match the selected filters.</span>}
          pagination={
            <Pager
              previous={!!history.length}
              next={!!page?.next_cursor}
              busy={busy || fetching}
              onPrevious={() => {
                setCursor(history[history.length - 1]);
                setHistory(history.slice(0, -1));
              }}
              onNext={() => {
                if (page?.next_cursor) {
                  setHistory([...history, cursor]);
                  setCursor(page.next_cursor);
                }
              }}
            />
          }
        />
      </div>
      <ExpandableSection headerText="Your editing permissions">
        <SpaceBetween size="s">
          <p>
            {identity.operator
              ? "DANS operators may change any RRset."
              : "All DNS data is readable. Only RRsets covered by an effective delegation may be changed."}{" "}
            Available actions are advisory; the API checks current authority for
            every write.
          </p>
          {!identity.operator && !grantsComplete && (
            <Alert type="info">
              Only the first page of your authority is loaded. Missing grants
              here do not mean access is denied. View all pages in My access;
              the API checks current authority on every write.
            </Alert>
          )}
          {!identity.operator && grantsComplete && !zoneGrants.length && (
            <p>No effective write delegations cover this zone.</p>
          )}
          {zoneGrants.map((grant) => (
            <div key={grant.id}>
              {grant.selectors
                .map((item) => item.kind + ": " + item.value)
                .join(", ")}{" "}
              · Types: {grant.record_types?.join(", ") || "all"} · Actions:{" "}
              {grant.change_kinds?.join(", ") || "all"}
            </div>
          ))}
          <p>
            Apex and literal wildcard owners require exact selectors. The editor
            checks current data before Save; another writer can still change it
            between that check and the write.
          </p>
        </SpaceBetween>
      </ExpandableSection>
      {editor && (
        <RecordEditor
          zone={zone}
          initial={editor.initial}
          deleting={editor.deleting}
          close={() => setEditor(null)}
          done={(message) => {
            setEditor(null);
            notify(message);
            void load();
          }}
        />
      )}
    </SpaceBetween>
  );
}
function ZoneForm({
  initial,
  close,
  done,
}: {
  initial: Zone | null;
  close: () => void;
  done: () => void;
}) {
  const [name, setName] = useState(initial?.name ?? ""),
    [kind, setKind] = useState(initial?.kind ?? "Native"),
    [masters, setMasters] = useState(initial?.masters?.join(", ") ?? ""),
    [nameservers, setNameservers] = useState(""),
    [account, setAccount] = useState(initial?.account ?? ""),
    [soa, setSOA] = useState(initial?.soa_edit_api ?? "DEFAULT"),
    [dnssec, setDNSSEC] = useState(initial?.dnssec ?? false),
    [rectify, setRectify] = useState(initial?.api_rectify ?? true),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function save() {
    if (busy) return;
    if (!name.endsWith(".")) {
      setError("Use an absolute zone name ending in a dot.");
      return;
    }
    setBusy(true);
    setError("");
    const values = (input: string) =>
      input
        .split(/[\n,]/)
        .map((value) => value.trim())
        .filter(Boolean);
    try {
      await request(
        initial ? zonePath(initial.id) : "/servers/localhost/zones",
        {
          method: initial ? "PUT" : "POST",
          body: JSON.stringify({
            ...(initial ? {} : { name, nameservers: values(nameservers) }),
            kind,
            masters: values(masters),
            account,
            soa_edit_api: soa,
            dnssec,
            api_rectify: rectify,
          }),
        },
      );
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
      header={initial ? "Edit zone" : "Create zone"}
      closeAriaLabel="Close zone form"
      onDismiss={() => {
        if (!busy) close();
      }}
      footer={
        <SpaceBetween direction="horizontal" size="xs">
          <Button onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={save} loading={busy}>
            Save
          </Button>
        </SpaceBetween>
      }
    >
      <SpaceBetween size="m">
        <Failure error={error} />
        <Field
          label="Zone name"
          value={name}
          onChange={setName}
          disabled={!!initial}
        />
        <Choice
          label="Zone kind"
          value={kind}
          onChange={setKind}
          options={["Native", "Master", "Slave", "Producer", "Consumer"].map(
            (value) => ({ label: value, value }),
          )}
        />
        {!initial && (
          <Field
            label="Nameservers"
            value={nameservers}
            onChange={setNameservers}
            description="Comma-separated absolute names."
          />
        )}
        <Field
          label="Primary servers"
          value={masters}
          onChange={setMasters}
          description="Comma-separated addresses for secondary zones."
        />
        <Field label="Account label" value={account} onChange={setAccount} />
        <Field label="SOA edit policy" value={soa} onChange={setSOA} />
        <Checkbox
          checked={dnssec}
          onChange={({ detail }) => setDNSSEC(detail.checked)}
        >
          DNSSEC
        </Checkbox>
        <Checkbox
          checked={rectify}
          onChange={({ detail }) => setRectify(detail.checked)}
        >
          Rectify automatically after changes
        </Checkbox>
      </SpaceBetween>
    </Modal>
  );
}
