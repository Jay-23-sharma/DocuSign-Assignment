import { useEffect, useState } from "react";
import type { PrepareDetail } from "../prepare/types";

type Event = {
  id: number;
  type: string;
  occurred_at: string;
  sender: string | null;
  recipient: string | null;
};
const names: Record<string, string> = {
  sent: "Sent",
  viewed: "Viewed",
  signed: "Signed",
  declined: "Declined",
  voided: "Voided",
  expired: "Expired",
  completed: "Completed",
  invitation_issued: "Invitation issued",
  invitation_accepted: "Invitation accepted by mail server",
  invitation_failed: "Invitation delivery failed",
};
export function History({ id, doc }: { id: string; doc: PrepareDetail }) {
  const [events, setEvents] = useState<Event[] | null>(null),
    [failed, setFailed] = useState(false),
    [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setFailed(false);
    setEvents(null);
    async function load() {
      try {
        const r = await fetch("/api/documents/" + id + "/history", {
          signal: controller.signal,
        });
        if (!r.ok) throw Error();
        const data = await r.json();
        if (!controller.signal.aborted) setEvents(data);
      } catch {
        if (!controller.signal.aborted) setFailed(true);
      }
    }
    void load();
    return () => controller.abort();
  }, [id, doc, retry]);
  return (
    <section>
      <h3>Document history</h3>
      {failed ? (
        <p role="alert">
          History could not be loaded.{" "}
          <button onClick={() => setRetry((n) => n + 1)}>Retry history</button>
        </p>
      ) : events === null ? (
        <p>Loading history…</p>
      ) : !events.length ? (
        <p>No events yet.</p>
      ) : (
        <ol aria-label="Document history">
          {events.map((e) => (
            <li key={e.id}>
              <strong>{names[e.type] ?? e.type}</strong>
              {e.sender && " · " + e.sender}
              {e.recipient && " · " + e.recipient} ·{" "}
              <time dateTime={e.occurred_at}>
                {new Date(e.occurred_at).toLocaleString()}
              </time>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
