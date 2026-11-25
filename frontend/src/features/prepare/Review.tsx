import { useState, type FormEvent } from "react";
import type { PrepareDetail } from "./types";

function localTime(value: string | null) {
  if (!value) return "";
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 19);
}

export function Review({ id, doc }: { id: string; doc: PrepareDetail }) {
  const base = "/api/documents/" + id;
  const [subject, setSubject] = useState(doc.email_subject ?? "");
  const [message, setMessage] = useState(doc.email_message ?? "");
  const [deadline, setDeadline] = useState(localTime(doc.expires_at));
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const release =
      (event.nativeEvent as SubmitEvent).submitter?.getAttribute("value") ===
      "send";
    setBusy(true);
    setNotice("");
    setError("");
    try {
      const response = await fetch(base, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email_subject: subject,
          email_message: message,
          expires_at: deadline ? new Date(deadline).toISOString() : null,
        }),
      });
      if (!response.ok)
        throw new Error(
          (await response.json()).error || "Settings could not be saved.",
        );
      if (!release) {
        setNotice("Settings saved.");
        return;
      }
      const sent = await fetch(base + "/send", { method: "POST" });
      if (!sent.ok)
        throw new Error(
          (await sent.json()).error || "Could not confirm release.",
        );
      window.location.assign("/documents/" + id);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Please try again.");
    } finally {
      setBusy(false);
    }
  }
  return (
    <section>
      <a href={"/documents/" + id + "/prepare"}>Back to Prepare</a>
      <h2>Review and send</h2>
      <h3>{doc.title}</h3>
      <table>
        <thead>
          <tr>
            <th>Recipient</th>
            <th>Email</th>
            <th>Fields</th>
            <th>Required signatures</th>
          </tr>
        </thead>
        <tbody>
          {doc.recipients.map((p) => {
            const fields = doc.fields.filter((f) => f.recipient_id === p.id);
            return (
              <tr key={p.id}>
                <td>{p.name}</td>
                <td>{p.email}</td>
                <td>{fields.length}</td>
                <td>
                  {
                    fields.filter(
                      (f) => f.required && f.field_type === "signature",
                    ).length
                  }
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <form onSubmit={submit} onChange={() => setNotice("")}>
        <label htmlFor="subject">Email subject</label>
        <input
          id="subject"
          value={subject}
          disabled={busy}
          onChange={(e) => setSubject(e.target.value)}
        />
        <label htmlFor="message">Message</label>
        <textarea
          id="message"
          rows={4}
          value={message}
          disabled={busy}
          onChange={(e) => setMessage(e.target.value)}
        />
        <label htmlFor="deadline">
          Signing deadline ({Intl.DateTimeFormat().resolvedOptions().timeZone})
        </label>
        <input
          id="deadline"
          type="datetime-local"
          step="1"
          value={deadline}
          disabled={busy}
          onChange={(e) => setDeadline(e.target.value)}
        />
        <button disabled={busy}>Save settings</button>
        <p>Sending locks the document, recipients, fields, and invitation settings.</p>
        <button value="send" disabled={busy}>
          Send invitations
        </button>
      </form>
      {notice && <p role="status">{notice}</p>}
      {error && (
        <div role="alert">
          {error}
          <a href={"/documents/" + id}>Check document status</a>
        </div>
      )}
    </section>
  );
}
