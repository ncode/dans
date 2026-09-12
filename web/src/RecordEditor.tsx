import { useState } from "react";
import Alert from "@cloudscape-design/components/alert";
import Button from "@cloudscape-design/components/button";
import Checkbox from "@cloudscape-design/components/checkbox";
import FormField from "@cloudscape-design/components/form-field";
import Header from "@cloudscape-design/components/header";
import Modal from "@cloudscape-design/components/modal";
import SpaceBetween from "@cloudscape-design/components/space-between";
import Textarea from "@cloudscape-design/components/textarea";
import { APIError, request, zonePath } from "./api.ts";
import { Failure, Field, messageOf } from "./controls.tsx";
import { sameRRset, validateRRset, type RRset, type Zone } from "./domain.ts";
export async function liveRRset(
  zone: Zone,
  name: string,
  type: string,
): Promise<RRset | null> {
  const query = new URLSearchParams({
    rrset_name: name,
    rrset_type: type,
    rrsets: "true",
    include_disabled: "true",
  });
  const response = await request<Zone>(zonePath(zone.id) + "?" + query);
  if (!Array.isArray(response.rrsets))
    throw new Error(
      "The live RRset response was incomplete. Try loading again.",
    );
  return (
    response.rrsets.find(
      (item) =>
        item.name.toLowerCase() === name.toLowerCase() &&
        item.type === type.toUpperCase(),
    ) ?? null
  );
}
export function RecordEditor({
  zone,
  initial,
  deleting,
  close,
  done,
}: {
  zone: Zone;
  initial: RRset | null;
  deleting: boolean;
  close: () => void;
  done: (message: string) => void;
}) {
  const [draft, setDraft] = useState<RRset>(() =>
    structuredClone(
      initial ?? {
        name: zone.name,
        type: "A",
        ttl: 300,
        records: [{ content: "", disabled: false }],
        comments: [],
      },
    ),
  );
  const [baseline, setBaseline] = useState(initial),
    [conflict, setConflict] = useState<{ current: RRset | null } | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [uncertain, setUncertain] = useState(false);
  const update = (change: Partial<RRset>) => setDraft({ ...draft, ...change });
  async function save() {
    if (busy || uncertain || conflict) return;
    const invalid = deleting ? "" : validateRRset(draft);
    if (invalid) {
      setError(invalid);
      return;
    }
    setBusy(true);
    setError("");
    try {
      const current = await liveRRset(zone, draft.name, draft.type);
      if (!sameRRset(baseline, current)) {
        setConflict({ current });
        return;
      }
      const change = deleting
        ? { name: draft.name, type: draft.type, changetype: "DELETE" }
        : { ...draft, changetype: "REPLACE" };
      await request(zonePath(zone.id), {
        method: "PATCH",
        body: JSON.stringify({ rrsets: [change] }),
      });
      done(deleting ? "RRset deleted." : "RRset saved.");
    } catch (error) {
      setError(messageOf(error));
      setUncertain(error instanceof APIError && error.uncertain);
    } finally {
      setBusy(false);
    }
  }
  async function check() {
    setBusy(true);
    try {
      setConflict({ current: await liveRRset(zone, draft.name, draft.type) });
      setUncertain(false);
      setError("");
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
      header={
        deleting ? "Delete RRset" : initial ? "Edit RRset" : "Create RRset"
      }
      onDismiss={() => {
        if (!busy) close();
      }}
      closeAriaLabel="Close RRset editor"
      footer={
        <SpaceBetween direction="horizontal" size="xs">
          <Button onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={uncertain || !!conflict}
            onClick={save}
          >
            {deleting ? "Delete RRset" : "Save"}
          </Button>
        </SpaceBetween>
      }
    >
      <SpaceBetween size="l">
        <Failure error={error} />
        {uncertain && (
          <Button onClick={check} loading={busy}>
            Check current state
          </Button>
        )}
        {deleting ? (
          <Alert type="warning">
            Delete {draft.name} {draft.type}, including every value and comment?
            This cannot be undone.
          </Alert>
        ) : (
          <>
            <div className="two-columns">
              <Field
                label="Owner name"
                value={draft.name}
                onChange={(name) => update({ name })}
                disabled={!!initial}
                description="Absolute DNS name, including the trailing dot."
              />
              <Field
                label="Record type"
                value={draft.type}
                onChange={(type) => update({ type: type.toUpperCase() })}
                disabled={!!initial}
              />
            </div>
            <Field
              label="TTL (seconds)"
              value={Number.isNaN(draft.ttl) ? "" : String(draft.ttl)}
              onChange={(value) =>
                update({ ttl: value === "" ? NaN : Number(value) })
              }
              type="number"
            />
            <Header variant="h3">Record values</Header>
            {draft.records.map((record, index) => (
              <div className="row-fields" key={index}>
                <FormField label={"Record value " + (index + 1)}>
                  <Textarea
                    value={record.content}
                    onChange={({ detail }) =>
                      update({
                        records: draft.records.map((item, i) =>
                          i === index
                            ? { ...item, content: detail.value }
                            : item,
                        ),
                      })
                    }
                  />
                </FormField>
                <Checkbox
                  checked={record.disabled ?? false}
                  onChange={({ detail }) =>
                    update({
                      records: draft.records.map((item, i) =>
                        i === index
                          ? { ...item, disabled: detail.checked }
                          : item,
                      ),
                    })
                  }
                >
                  Disabled
                </Checkbox>
                <Button
                  onClick={() =>
                    update({
                      records: draft.records.filter((_, i) => i !== index),
                    })
                  }
                  ariaLabel={"Remove record value " + (index + 1)}
                >
                  Remove
                </Button>
              </div>
            ))}
            <Button
              onClick={() =>
                update({
                  records: [...draft.records, { content: "", disabled: false }],
                })
              }
            >
              Add value
            </Button>
            <Header variant="h3">Comments</Header>
            {(draft.comments ?? []).map((comment, index) => (
              <div key={index}>
                <div className="two-columns">
                  <FormField label={"Comment " + (index + 1)}>
                    <Textarea
                      value={comment.content ?? ""}
                      onChange={({ detail }) =>
                        update({
                          comments: draft.comments?.map((item, i) =>
                            i === index
                              ? { ...item, content: detail.value }
                              : item,
                          ),
                        })
                      }
                    />
                  </FormField>
                  <Field
                    label={"Comment author " + (index + 1)}
                    value={comment.account ?? ""}
                    onChange={(account) =>
                      update({
                        comments: draft.comments?.map((item, i) =>
                          i === index ? { ...item, account } : item,
                        ),
                      })
                    }
                  />
                </div>
                <Button
                  onClick={() =>
                    update({
                      comments: draft.comments?.filter((_, i) => i !== index),
                    })
                  }
                >
                  Remove comment {index + 1}
                </Button>
              </div>
            ))}
            <Button
              onClick={() =>
                update({
                  comments: [
                    ...(draft.comments ?? []),
                    { content: "", account: "" },
                  ],
                })
              }
            >
              Add comment
            </Button>
          </>
        )}
        {conflict && (
          <Alert
            type="warning"
            header="This RRset changed while you were editing."
          >
            <SpaceBetween size="m">
              <p>
                Your draft is preserved. Review the current RRset before
                choosing whether to replace it. The check does not lock
                concurrent writers.
              </p>
              <div className="two-columns">
                <div>
                  <strong>Current live RRset</strong>
                  <pre className="dns-value">
                    {conflict.current
                      ? JSON.stringify(conflict.current, null, 2)
                      : "RRset does not exist."}
                  </pre>
                </div>
                <div>
                  <strong>
                    {deleting ? "RRset selected for deletion" : "Your draft"}
                  </strong>
                  <pre className="dns-value">
                    {JSON.stringify(draft, null, 2)}
                  </pre>
                </div>
              </div>
              <Button
                onClick={() => {
                  setBaseline(conflict.current);
                  setConflict(null);
                }}
              >
                Keep draft and use current as baseline
              </Button>
              {conflict.current && !deleting && (
                <Button
                  onClick={() => {
                    setDraft(structuredClone(conflict.current!));
                    setBaseline(conflict.current);
                    setConflict(null);
                  }}
                >
                  Replace draft with current
                </Button>
              )}
            </SpaceBetween>
          </Alert>
        )}
      </SpaceBetween>
    </Modal>
  );
}
