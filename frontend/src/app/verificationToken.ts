// Capture the fragment once before React renders, including under Strict Mode.
export function takeToken({ location, history }: Pick<Window, "location" | "history">): string {
  const token = location.hash.slice(1);
  if (location.hash) history.replaceState(history.state, "", location.pathname + location.search);
  return token;
}
