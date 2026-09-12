import { useEffect, useState } from "react";
import Alert from "@cloudscape-design/components/alert";
import Button from "@cloudscape-design/components/button";
import Header from "@cloudscape-design/components/header";
import Link from "@cloudscape-design/components/link";
import Modal from "@cloudscape-design/components/modal";
import SpaceBetween from "@cloudscape-design/components/space-between";
import Table from "@cloudscape-design/components/table";
import { request } from "./api.ts";
import { Choice, Failure, Field } from "./controls.tsx";
import {
  ConfirmAction,
  managementError,
  managementPath,
  queryPath,
  usePage,
  useDetailHeading,
} from "./management-controls.tsx";
import type { Binding } from "./domain.ts";

export function Bindings({
  route,
  notify,
}: {
  route: string;
  notify: (message: string) => void;
}) {
  let id = "";
  try {
    id = decodeURIComponent(route.split("/")[2] || "");
  } catch {
    /* Invalid route shows collection. */
  }
  return id ? (
    <BindingDetails key={id} id={id} notify={notify} />
  ) : (
    <BindingList notify={notify} />
  );
}
function BindingList({ notify }: { notify: (message: string) => void }) {
  const [status, setStatus] = useState(""),
    [create, setCreate] = useState(false);
  const page = usePage<Binding>(queryPath("/dans/zone-bindings", { status }));
  return (
    <SpaceBetween size="m">
      <Failure error={page.error} onRetry={page.reload} />
      <Table
        variant="full-page"
        wrapLines
        trackBy="id"
        items={page.items}
        loading={page.loading}
        loadingText="Loading zone bindings"
        header={
          <Header
            variant="h1"
            description="Bindings associate access with one zone lifetime. Retired lifetimes never regain their delegations."
            actions={
              <Button variant="primary" onClick={() => setCreate(true)}>
                Create binding
              </Button>
            }
          >
            Zone bindings
          </Header>
        }
        filter={
          <Choice
            label="Binding state"
            value={status}
            onChange={setStatus}
            options={[
              { value: "", label: "All states" },
              { value: "active", label: "Active" },
              { value: "retired", label: "Retired" },
            ]}
          />
        }
        columnDefinitions={[
          {
            id: "zone",
            header: "Zone",
            cell: (item) => (
              <Link href={"#/bindings/" + item.id}>{item.zone_name}</Link>
            ),
          },
          {
            id: "generation",
            header: "Generation",
            cell: (item) => item.generation,
          },
          { id: "state", header: "State", cell: (item) => item.status },
          { id: "id", header: "Binding ID", cell: (item) => item.id },
        ]}
        pagination={page.pagination}
        empty="No bindings match."
      />
      {create && (
        <CreateBinding
          close={() => setCreate(false)}
          done={(binding) => {
            setCreate(false);
            notify("Zone binding created.");
            location.hash = "#/bindings/" + binding.id;
          }}
        />
      )}
    </SpaceBetween>
  );
}
function CreateBinding({
  close,
  done,
}: {
  close: () => void;
  done: (binding: Binding) => void;
}) {
  const [zone, setZone] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  return (
    <Modal
      visible
      header="Create binding"
      closeAriaLabel="Close binding creation"
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
              if (!zone) {
                setError("Enter the current zone ID.");
                return;
              }
              setBusy(true);
              try {
                const binding = await request<Binding>("/dans/zone-bindings", {
                  method: "POST",
                  body: JSON.stringify({ zone_id: zone }),
                });
                if (!binding?.id)
                  throw new Error(
                    "The binding was created, but its details could not be read. Reload the list before another write.",
                  );
                done(binding);
              } catch (error) {
                setError(managementError(error));
              } finally {
                setBusy(false);
              }
            }}
          >
            Create binding
          </Button>
        </SpaceBetween>
      }
    >
      <SpaceBetween size="m">
        <Field
          label="Zone ID"
          value={zone}
          onChange={setZone}
          description="Use the exact ID of an existing upstream zone, including its final dot."
        />
        <Alert type="warning">
          Create access metadata for this current zone lifetime. Existing
          retired bindings require explicit recovery and never restore old
          delegations. The API checks eligibility before creating a binding.
        </Alert>
        <Failure error={error} />
      </SpaceBetween>
    </Modal>
  );
}
const actions = {
  confirm_absent: "Confirm absence",
  retry_delete: "Retry deletion",
  rebind: "Rebind zone",
} as const;
type Recovery = keyof typeof actions;
function BindingDetails({
  id,
  notify,
}: {
  id: string;
  notify: (message: string) => void;
}) {
  const [binding, setBinding] = useState<Binding | null>(null),
    [error, setError] = useState(""),
    [observing, setObserving] = useState(false),
    [observation, setObservation] = useState<{
      zone_present: boolean;
      observed_zone_id: string | null;
      observed_at: string;
    } | null>(null),
    [action, setAction] = useState<Recovery | null>(null);
  const path = managementPath("zone-bindings", id);
  async function load() {
    try {
      setBinding(await request<Binding>(path));
      setError("");
    } catch (error) {
      setError(managementError(error));
    }
  }
  useEffect(() => {
    void load();
  }, [id]);
  useDetailHeading(binding?.zone_name);
  async function observe() {
    if (observing) return;
    setObserving(true);
    setError("");
    setObservation(null);
    try {
      setObservation(await request(path + "/observe", { method: "POST" }));
    } catch (error) {
      setError(managementError(error));
    } finally {
      setObserving(false);
    }
  }
  return (
    <SpaceBetween size="l">
      <Header
        variant="h1"
        description="Zone binding details"
        actions={<Button onClick={load}>Reload binding</Button>}
      >
        {binding?.zone_name || "Zone binding"}
      </Header>
      <Failure error={error} />
      {binding && (
        <>
          <dl className="details">
            <dt>Binding ID</dt>
            <dd>{binding.id}</dd>
            <dt>Zone ID</dt>
            <dd>{binding.zone_id}</dd>
            <dt>Generation</dt>
            <dd>{binding.generation}</dd>
            <dt>State</dt>
            <dd>{binding.status}</dd>
            <dt>Deletion state</dt>
            <dd>
              {binding.deletion_state?.replaceAll("_", " ") ||
                "Unavailable; reload to inspect eligibility"}
            </dd>
          </dl>
          <Alert type="info">
            Recovery eligibility is advisory and is checked again at submission.
            Observation reads upstream state and does not recover a binding.
            Retired bindings and their delegations remain retired.
          </Alert>
          <SpaceBetween direction="horizontal" size="xs">
            <Button loading={observing} onClick={observe}>
              Observe upstream
            </Button>
            {(Object.keys(actions) as Recovery[])
              .filter((value) => binding.recovery_actions?.includes(value))
              .map((value) => (
                <Button key={value} onClick={() => setAction(value)}>
                  {actions[value]}
                </Button>
              ))}
          </SpaceBetween>
          {observation && (
            <Alert type="info">
              {observation.zone_present
                ? "Zone is present upstream: " + observation.observed_zone_id
                : "Zone is absent upstream."}{" "}
              Observed {new Date(observation.observed_at).toLocaleString()}.
              This observation can become stale.
            </Alert>
          )}
          {action && (
            <ConfirmAction
              title={actions[action]}
              close={() => setAction(null)}
              action={async () => {
                const suffix = {
                  confirm_absent: "confirm-absent",
                  retry_delete: "retry-delete",
                  rebind: "rebind",
                }[action];
                const result = await request<Binding>(path + "/" + suffix, {
                  method: "POST",
                  ...(action === "rebind"
                    ? { body: JSON.stringify({ zone_id: binding.zone_id }) }
                    : {}),
                });
                if (action === "rebind" && result?.id)
                  location.hash = "#/bindings/" + result.id;
              }}
              done={() => {
                setAction(null);
                setObservation(null);
                notify("Binding recovery action succeeded.");
                void load();
              }}
            >
              {action === "retry_delete"
                ? "Submit one new deletion attempt for this upstream zone? This can delete every RRset. The old binding and its delegations remain retired. Unknown outcomes must be investigated before another explicit attempt."
                : action === "confirm_absent"
                  ? "Verify the zone is absent upstream and record its absence? The old binding and its delegations remain retired. If the zone is present, the API rejects this action."
                  : "Create a new lifetime for this current upstream zone? The old binding stays retired. This does not copy or restore its delegations; create new delegations separately."}
            </ConfirmAction>
          )}
        </>
      )}
    </SpaceBetween>
  );
}
