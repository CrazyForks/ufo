// Thin fetch helpers over the same-origin /api/v1 facade.

export function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  return fetch(path, init);
}

export function fleetPath(fleet: string, path: string): string {
  return `/api/v1/fleets/${encodeURIComponent(fleet)}${path}`;
}

export function withFleet(path: string, fleet: string): string {
  return `${path}${path.includes("?") ? "&" : "?"}fleet_id=${encodeURIComponent(fleet)}`;
}

export async function getJSON<T>(path: string): Promise<T | null> {
  const res = await apiFetch(path, { cache: "no-store" });
  return res.ok ? ((await res.json()) as T) : null;
}

export async function postJSON(path: string, body?: unknown): Promise<Response> {
  return apiFetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body == null ? undefined : JSON.stringify(body),
  });
}

export async function postForm(path: string, body: FormData): Promise<Response> {
  return apiFetch(path, { method: "POST", body });
}

export async function putRaw(path: string, body: BodyInit, headers?: Record<string, string>): Promise<Response> {
  return apiFetch(path, { method: "PUT", headers, body });
}

export async function patchJSON(path: string, body?: unknown): Promise<Response> {
  return apiFetch(path, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: body == null ? undefined : JSON.stringify(body),
  });
}

export async function putJSON(path: string, body?: unknown): Promise<Response> {
  return apiFetch(path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: body == null ? undefined : JSON.stringify(body),
  });
}

export async function del(path: string): Promise<Response> {
  return apiFetch(path, { method: "DELETE" });
}

export function maxExpiryDate(): string {
  const d = new Date();
  d.setFullYear(d.getFullYear() + 1);
  return d.toISOString().slice(0, 10);
}
