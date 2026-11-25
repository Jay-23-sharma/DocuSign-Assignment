import { useState } from "react";
import type { PrepareDetail } from "../prepare/types";

const labels = {
  accepted: "Accepted by mail server",
  failed: "Failed",
  unconfirmed: "Delivery unconfirmed",
};

export function DeliveryResults({
  id,
  doc,
  onRefresh,
}: {
  id: string;
  doc: PrepareDetail;
  onRefresh: (next: PrepareDetail) => void;
}) {
  const [pending, setPending] = useState<number | null>(null);
  const [error, setError] = useState("");
  const base = "/api/documents/" + id;
  const open =
    ["sent", "viewed"].includes(doc.status) &&
    doc.expires_at &&
    Date.parse(doc.expires_at) > Date.now();
  async function resend(recipient: number) {
    setPending(recipient);
    setError("");
    try {
      const response = await fetch(
        base + "/recipients/" + recipient + "/resend",
        { method: "POST" },
      );
      if (!response.ok)
        throw new Error(
          (await response.json()).error || "Resend could not be confirmed.",
        );
      const latest = await fetch(base);
      if (!latest.ok) throw new Error("Reload to check delivery.");
      onRefresh(await latest.json());
    } catch (e) {
      setError(e instanceof Error ? e.message : "Reload to check delivery.");
    } finally {
      setPending(null);
    }
  }
  return (
    <section aria-label="Invitation delivery">
      <h3>Invitation delivery</h3>
      <p>
        {doc.recipients.filter((r) => r.status === "signed").length} of{" "}
        {doc.recipients.length} recipients signed
      </p>
      <p>{doc.email_subject}</p>
      {doc.expires_at && (
        <p>
          Signing deadline:{" "}
          <time dateTime={doc.expires_at}>
            {new Date(doc.expires_at).toLocaleString()}
          </time>
        </p>
      )}
      <table>
        <thead>
          <tr>
            <th>Recipient</th>
            <th>Signing status</th>
            <th>Delivery</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {doc.recipients.map((p) => (
            <tr key={p.id}>
              <td>
                {p.name}
                <br />
                {p.email}
              </td>
              <td>{p.status}</td>
              <td>{labels[p.delivery_status ?? "unconfirmed"]}</td>
              <td>
                {open && ["pending", "viewed"].includes(p.status ?? "") && (
                  <button
                    disabled={pending !== null}
                    onClick={() => void resend(p.id)}
                    aria-label={"Resend to " + p.name}
                  >
                    {pending === p.id ? "Sending…" : "Resend"}
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
