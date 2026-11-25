import { useState, type FormEvent } from "react";
import type { Recipient } from "./types";

type Props = {
  base: string;
  recipients: Recipient[];
  busy: boolean;
  setBusy: (busy: boolean) => void;
  onSave: (recipient: Recipient) => void;
  onDelete: (id: number) => void;
  deletionMessage: (id: number, savedCount: number) => string;
};

export function Recipients({
  base,
  recipients,
  busy,
  setBusy,
  onSave,
  onDelete,
  deletionMessage,
}: Props) {
  const [edit, setEdit] = useState<Recipient | null>(null);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    setBusy(true);
    setError("");
    try {
      const response = await fetch(
        base + "/recipients" + (edit ? "/" + edit.id : ""),
        {
          method: edit ? "PATCH" : "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            name: data.get("name"),
            email: data.get("email"),
          }),
        },
      );
      const result = await response.json();
      if (!response.ok)
        throw new Error(result.error || "Recipient could not be saved.");
      onSave(result);
      form.reset();
      setEdit(null);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Recipient could not be saved.",
      );
    } finally {
      setBusy(false);
    }
  }
  async function remove(person: Recipient) {
    setBusy(true);
    setError("");
    try {
      const response = await fetch(base);
      if (!response.ok)
        throw new Error("Could not check saved fields. Try again.");
      const doc = await response.json();
      const count = (doc.fields ?? []).filter(
        (f: { recipient_id: number }) => f.recipient_id === person.id,
      ).length;
      if (
        !window.confirm(
          "Remove " + person.name + "? " + deletionMessage(person.id, count),
        )
      )
        return;
      const deleted = await fetch(
        base + "/recipients/" + person.id + "?savedFields=" + count,
        { method: "DELETE" },
      );
      if (!deleted.ok)
        throw new Error(
          (await deleted.json()).error || "Recipient could not be removed.",
        );
      if (edit?.id === person.id) setEdit(null);
      onDelete(person.id);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Recipient could not be removed.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <section>
      <h3>Recipients</h3>
      {recipients.map((person) => (
        <div className="recipient-row" key={person.id}>
          <strong>{person.name}</strong>
          <small>{person.email}</small>
          <button
            disabled={busy}
            aria-label={"Edit " + person.name}
            onClick={() => {
              setEdit(person);
              setError("");
            }}
          >
            Edit
          </button>
          <button
            disabled={busy}
            aria-label={"Remove " + person.name}
            onClick={() => void remove(person)}
          >
            Remove
          </button>
        </div>
      ))}
      <form key={edit?.id ?? "new"} onSubmit={submit}>
        <label>
          Recipient name
          <input
            name="name"
            disabled={busy}
            defaultValue={edit?.name}
            required
          />
        </label>
        <label>
          Recipient email
          <input
            name="email"
            disabled={busy}
            type="email"
            defaultValue={edit?.email}
            required
          />
        </label>
        <button disabled={busy}>
          {edit ? "Save recipient" : "Add recipient"}
        </button>
        {edit && (
          <button type="button" disabled={busy} onClick={() => setEdit(null)}>
            Cancel edit
          </button>
        )}
      </form>
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
