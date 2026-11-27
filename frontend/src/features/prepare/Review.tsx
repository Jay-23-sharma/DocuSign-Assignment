import { useState, type FormEvent } from "react";
import { RecipientPreview } from "./RecipientPreview";
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
  const storageKey = "localsign-review-" + id;
  const saved = { subject: doc.email_subject ?? "", message: doc.email_message ?? "", deadline: localTime(doc.expires_at) };
  const baseline = JSON.stringify(saved);
  const [settings, setSettings] = useState(() => {
    try {
      const draft = JSON.parse(sessionStorage.getItem(storageKey) ?? "null");
      if (draft?.baseline === baseline && typeof draft.subject === "string" &&
          typeof draft.message === "string" && typeof draft.deadline === "string") {
        return { subject: draft.subject as string, message: draft.message as string, deadline: draft.deadline as string };
      }
    } catch { /* Tab storage may be unavailable. */ }
    return saved;
  });
  const { subject, message, deadline } = settings;
  function changeSetting(change: Partial<typeof settings>) {
    const next = { ...settings, ...change };
    setSettings(next);
    try { sessionStorage.setItem(storageKey, JSON.stringify({ ...next, baseline: savedBaseline })); }
    catch { /* Review and preview still share the in-memory draft. */ }
  }
  const [savedBaseline, setSavedBaseline] = useState(baseline);
  const [previewId, setPreviewId] = useState(() => Number(new URLSearchParams(location.search).get("recipient")));
  function showPreview(recipientId: number) {
    const url = new URL(location.href);
    if (recipientId) url.searchParams.set("recipient", String(recipientId));
    else url.searchParams.delete("recipient");
    history.replaceState(history.state, "", url);
    setPreviewId(recipientId);
  }
  const recipient = doc.recipients.find(person => person.id === previewId);
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
      setSavedBaseline(JSON.stringify(settings));
      try { sessionStorage.removeItem(storageKey); } catch { /* Storage unavailable. */ }
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
  if (recipient) return <RecipientPreview key={recipient.id} id={id} doc={doc} recipient={recipient}
    switchTo={showPreview} back={() => showPreview(0)} />;
  return (
    <section>
      <a className="document-action-link" href={"/documents/" + id + "/prepare"}>Back to Prepare</a>
      <h2>Review and send</h2>
      <section className="review-recipients" aria-labelledby="review-document-title">
        <h3 id="review-document-title">{doc.title}</h3>
        <div className="review-table-scroll">
          <table>
            <thead>
              <tr>
                <th>Recipient</th>
                <th>Email</th>
                <th>Fields</th>
                <th>Required signatures</th>
                <th>Sender preview</th>
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
                    <td><button title={`Preview as ${p.name}`} disabled={busy} onClick={() => showPreview(p.id)}>Preview as {p.name}</button></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
      <form onSubmit={submit} onChange={() => setNotice("")}>
        <label htmlFor="subject">Email subject</label>
        <input
          id="subject"
          value={subject}
          disabled={busy}
          onChange={(e) => changeSetting({ subject: e.target.value })}
        />
        <label htmlFor="message">Message</label>
        <textarea
          id="message"
          rows={4}
          value={message}
          disabled={busy}
          onChange={(e) => changeSetting({ message: e.target.value })}
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
          onChange={(e) => changeSetting({ deadline: e.target.value })}
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
