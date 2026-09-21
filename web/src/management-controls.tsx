import { useEffect, useRef, useState, type ReactNode } from "react";
import Alert from "@cloudscape-design/components/alert";
import Button from "@cloudscape-design/components/button";
import Modal from "@cloudscape-design/components/modal";
import SpaceBetween from "@cloudscape-design/components/space-between";
import { APIError, request } from "./api.ts";
import { Choice, Failure, Field, Pager, messageOf } from "./controls.tsx";
import type { Group, Identity, Page } from "./domain.ts";

export const managementPath = (kind: string, id: string) =>
  "/dans/" + kind + "/" + encodeURIComponent(id);
// Detail headings arrive after the route change, once their read finishes.
export function useDetailHeading(title: string | undefined) {
  useEffect(() => {
    if (!title) return;
    document.title = title + " · DANS";
    const frame = requestAnimationFrame(() => {
      const heading = document.querySelector<HTMLElement>("#main-content h1");
      if (heading) {
        heading.tabIndex = -1;
        heading.focus();
      }
    });
    return () => cancelAnimationFrame(frame);
  }, [title]);
}
export function queryPath(path: string, values: Record<string, string>) {
  const query = new URLSearchParams(
    Object.entries(values).filter(([, value]) => value !== ""),
  );
  return path + (path.includes("?") ? "&" : "?") + query;
}
// Retain one response page and cursor positions, never an accumulated collection.
export function usePage<T>(path: string) {
  const [position, setPosition] = useState({
    path,
    cursor: "",
    history: [] as string[],
  });
  const [result, setResult] = useState<{ path: string; page: Page<T> } | null>(
    null,
  );
  const [loading, setLoading] = useState(true),
    [error, setError] = useState(""),
    [revision, setRevision] = useState(0);
  const cursor = position.path === path ? position.cursor : "";
  const history = position.path === path ? position.history : [];
  const sequence = useRef(0);
  useEffect(() => {
    const controller = new AbortController(),
      current = ++sequence.current;
    setLoading(true);
    setError("");
    request<Page<T>>(queryPath(path, { limit: "100", cursor }), {
      signal: controller.signal,
    })
      .then((page) => {
        if (current === sequence.current && !controller.signal.aborted)
          setResult({ path, page });
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(managementError(error));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [path, cursor, revision]);
  const page = result?.path === path ? result.page : null;
  return {
    items: page?.items ?? [],
    loading,
    error,
    reload: () => setRevision((value) => value + 1),
    pagination: (
      <Pager
        previous={!!history.length}
        next={!!page?.next_cursor}
        busy={loading || !!error}
        onPrevious={() =>
          setPosition({
            path,
            cursor: history.at(-1) || "",
            history: history.slice(0, -1),
          })
        }
        onNext={() => {
          if (page?.next_cursor)
            setPosition({
              path,
              cursor: page.next_cursor,
              history: [...history, cursor],
            });
        }}
      />
    ),
  };
}
export function managementError(error: unknown) {
  return error instanceof APIError && error.status === 403
    ? "Your current authority does not allow this action. Reload your profile to check your access. " +
        error.message
    : messageOf(error);
}
export function RemoteSelector({
  kind,
  label,
  value,
  onChange,
  enabledOnly = false,
}: {
  kind: "identities" | "groups";
  label: string;
  value: string;
  onChange: (id: string) => void;
  enabledOnly?: boolean;
}) {
  const [prefix, setPrefix] = useState(""),
    [search, setSearch] = useState("");
  const page = usePage<Identity | Group>(
    queryPath("/dans/" + kind, {
      handle_prefix: search,
      enabled: enabledOnly ? "true" : "",
    }),
  );
  const [selected, setSelected] = useState<{
    value: string;
    label: string;
  } | null>(null);
  const options = page.items.map((item) => ({
    value: item.id,
    label:
      (item.display_name || item.handle) +
      " (" +
      item.handle +
      ")" +
      (!item.enabled ? " · Disabled" : ""),
  }));
  if (
    selected &&
    selected.value === value &&
    !options.some((item) => item.value === value)
  )
    options.unshift(selected);
  return (
    <SpaceBetween size="s">
      <div className="filter-grid">
        <Field
          label={
            (kind === "identities" ? "Identity" : "Group") + " handle prefix"
          }
          value={prefix}
          onChange={setPrefix}
          description="Literal starts-with search across all matching resources."
        />
        <Button
          onClick={() => {
            setSearch(prefix);
            if (prefix === search) page.reload();
          }}
          disabled={page.loading}
        >
          Search {kind}
        </Button>
      </div>
      <Failure error={page.error} onRetry={page.reload} />
      <Choice
        label={label}
        value={value}
        options={options}
        onChange={(id) => {
          setSelected(options.find((item) => item.value === id) || null);
          onChange(id);
        }}
      />
      {page.loading && <span role="status">Loading {kind}…</span>}
      {!page.loading && !page.items.length && <p>No matching {kind}.</p>}
      <div role="group" aria-label={label + " pages"}>
        {page.pagination}
      </div>
    </SpaceBetween>
  );
}
export function ConfirmAction({
  title,
  children,
  action,
  close,
  done,
}: {
  title: string;
  children: ReactNode;
  action: () => Promise<void>;
  close: () => void;
  done: () => void;
}) {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await action();
      done();
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
          <Button variant="primary" loading={busy} onClick={submit}>
            {title}
          </Button>
        </SpaceBetween>
      }
    >
      <SpaceBetween size="m">
        <Alert type="warning">{children}</Alert>
        <Failure error={error} />
      </SpaceBetween>
    </Modal>
  );
}
