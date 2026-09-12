export class APIError extends Error {
  status: number;
  uncertain: boolean;
  constructor(message: string, status = 0, uncertain = false) {
    super(message);
    this.status = status;
    this.uncertain = uncertain;
  }
}
export async function request<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const mutation = !["GET", "HEAD"].includes(init.method ?? "GET");
  let response: Response;
  try {
    response = await fetch("/api/v1" + path, {
      ...init,
      credentials: "same-origin",
      redirect: "error",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        ...(init.body ? { "Content-Type": "application/json" } : {}),
        ...init.headers,
      },
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError")
      throw error;
    throw new APIError(
      mutation
        ? "Outcome unknown. Read the current state before deciding what to do next. This request was not retried."
        : "Could not reach the API. Try loading again.",
      0,
      mutation,
    );
  }
  if (!response.ok) {
    let detail = response.statusText;
    try {
      const body = await response.json();
      detail =
        [body.error, ...(body.errors ?? [])].filter(Boolean).join(" — ") ||
        detail;
    } catch {
      /* The status still identifies an unsuccessful response. */
    }
    const uncertain =
      mutation && (response.status >= 500 || response.status === 408);
    if (
      response.status === 401 &&
      path !== "/dans/session" &&
      typeof window !== "undefined"
    )
      window.dispatchEvent(new Event("console:unauthorized"));
    throw new APIError(
      uncertain
        ? "Outcome unknown. Read the current state before another write. This request was not retried."
        : detail,
      response.status,
      uncertain,
    );
  }
  if (response.status === 204) return undefined as T;
  try {
    return (await response.json()) as T;
  } catch {
    // A received successful mutation status is conclusive even if its optional body is incomplete.
    if (mutation) return undefined as T;
    throw new APIError(
      "The API response was incomplete. Try loading again.",
      response.status,
    );
  }
}
export const zonePath = (id: string) =>
  "/servers/localhost/zones/" + encodeURIComponent(id);
export const browsePath = (id: string) =>
  "/dans/servers/localhost/zones/" + encodeURIComponent(id) + "/rrsets";
