export async function post(path: string, body: object): Promise<Response> {
  return fetch("/api/auth/" + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export function getSender(signal: AbortSignal): Promise<Response> {
  return fetch("/api/auth/me", { signal });
}
