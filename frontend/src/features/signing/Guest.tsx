import { lazy, Suspense, useEffect, useState } from "react";
import { boxStyle } from "../../components/pdf/fieldGeometry";
import { SignatureInput } from "./SignatureInput";
import type { Field } from "../prepare/types";

const PdfViewer = lazy(() => import("../../components/pdf/PdfViewer"));
type Assigned = Field & { id: number };
type GuestDoc = {
  title: string;
  recipient_name: string;
  page_count: number;
  fields: Assigned[];
};
type Answer = {
  id: number;
  text?: string;
  checked?: boolean;
  method?: string;
  reference?: string;
};

class ClosedAccess extends Error {}

async function request(path: string, session: number, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  if (session) headers.set("X-Signing-Session", String(session));
  const result = await fetch("/api/signing/" + path, {
    ...init,
    headers,
    cache: "no-store",
  });
  if (!result.ok) {
    const message = (await result.json()).error || "Request failed. Try again.";
    if (result.status === 403 || result.status === 409)
      throw new ClosedAccess(message);
    throw new Error(message);
  }
  return result;
}
function post(data: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  };
}
export function Guest() {
  const token = location.hash.slice(1);
  const [session, setSession] = useState<number>(
    token ? 0 : history.state?.guestSession || 0,
  );
  const [state, setState] = useState(session ? "loading" : "landing");
  const [doc, setDoc] = useState<GuestDoc | null>(null),
    [code, setCode] = useState("");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const [page, setPage] = useState(1),
    [selected, select] = useState(0);
  const [answers, setAnswers] = useState<Record<number, Answer>>({});
  const [confirmDecline, setConfirmDecline] = useState(false);
  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      if (e instanceof ClosedAccess && state === "verified") {
        setState("closed");
        setDoc(null);
        setAnswers({});
        setNotice("");
      }
      setError(
        e instanceof Error ? e.message : "Unable to connect. Try again.",
      );
    } finally {
      setBusy(false);
    }
  }
  async function load(id: number) {
    const response = await request("session", id);
    const { state: phase } = await response.json();
    if (phase === "verified") {
      const data: GuestDoc = await (await request("document", id)).json();
      setDoc(data);
      select(data.fields[0]?.id || 0);
      setPage(data.fields[0]?.page_number || 1);
    }
    setState(phase);
    setNotice("");
  }
  useEffect(() => {
    if (session) void run(() => load(session));
  }, []);
  async function sendCode(id: number) {
    await request("session/email-code", id, post({}));
    setNotice("Check your email for a six-digit code.");
  }
  async function open() {
    const { session_id } = await (
      await request("sessions", 0, post({ token }))
    ).json();
    history.replaceState({ guestSession: session_id }, "", "/sign");
    setSession(session_id);
    setState("code");
    await sendCode(session_id);
  }
  async function verify() {
    await request("session/verify-code", session, post({ code }));
    setCode("");
    await load(session);
  }
  function choose(f: Assigned) {
    select(f.id);
    setPage(f.page_number);
  }
  function update(a: Answer) {
    setAnswers((old) => ({ ...old, [a.id]: a }));
  }
  async function upload(id: number, method: string, file: Blob) {
    if (file.size > 2 * 1024 * 1024)
      throw new Error("Choose an image up to 2 MiB.");
    const result = await request("signatures?method=" + method, session, {
      method: "POST",
      body: file,
    });
    const { reference } = await result.json();
    update({ id, method, reference });
  }
  const field = doc?.fields.find((f) => f.id === selected);
  function next() {
    const missing = doc?.fields.filter((f) => {
      const a = answers[f.id];
      return f.required && !(a?.text?.trim() || a?.checked || a?.reference);
    });
    if (missing?.length) {
      choose(missing[0]);
      setNotice("Complete the selected required field.");
    } else setNotice("All required fields are filled.");
  }
  async function finish(decline: boolean) {
    const filled = Object.values(answers).filter(
      (a) => a.checked !== undefined || a.text?.trim() || a.reference,
    );
    await request(
      decline ? "decline" : "submit",
      session,
      post(decline ? { confirm: true } : { answers: filled }),
    );
    setState(decline ? "declined" : "signed");
    setAnswers({});
    setDoc(null);
  }
  return (
    <main className={doc ? "workspace" : ""}>
      <h1>{doc?.title || "Document invitation"}</h1>
      {state === "landing" &&
        (token ? (
          <button disabled={busy} onClick={() => void run(open)}>
            Open document
          </button>
        ) : (
          <p>Open the personal invitation from your email.</p>
        ))}
      {state === "loading" &&
        (busy ? (
          <p>Loading signing session…</p>
        ) : (
          <button onClick={() => void run(() => load(session))}>Retry</button>
        ))}
      {state === "code" && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void run(verify);
          }}
        >
          <label>
            Email code{" "}
            <input
              autoComplete="one-time-code"
              inputMode="numeric"
              pattern="[0-9]{6}"
              maxLength={6}
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
          </label>
          <button disabled={busy}>Verify code</button>
          <button
            type="button"
            disabled={busy}
            onClick={() => void run(() => sendCode(session))}
          >
            Send another code
          </button>
        </form>
      )}
      {state === "verified" && doc && (
        <>
          <p>Signing as {doc.recipient_name}</p>
          <div className="prepare-grid">
            <aside>
              <button onClick={next}>Next required field</button>
              <label>
                Assigned field{" "}
                <select
                  value={selected}
                  onChange={(e) =>
                    choose(
                      doc.fields.find((f) => f.id === Number(e.target.value))!,
                    )
                  }
                >
                  {doc.fields.map((f, i) => (
                    <option key={f.id} value={f.id}>
                      {i + 1}. {f.field_type} · page {f.page_number}
                      {f.required ? " · required" : ""}
                    </option>
                  ))}
                </select>
              </label>
              {field && (
                <fieldset key={field.id} disabled={busy}>
                  <legend>
                    {field.field_type}
                    {field.required ? " (required)" : ""}
                  </legend>
                  {field.field_type === "signature" ? (
                    <SignatureInput
                      value={answers[field.id] || {}}
                      busy={busy}
                      onChange={(v) => update({ id: field.id, ...v })}
                      upload={(method, file) =>
                        void run(() => upload(field.id, method, file))
                      }
                    />
                  ) : field.field_type === "checkbox" ? (
                    <label>
                      <input
                        type="checkbox"
                        checked={answers[field.id]?.checked || false}
                        onChange={(e) =>
                          update({ id: field.id, checked: e.target.checked })
                        }
                      />{" "}
                      Check this box
                    </label>
                  ) : (
                    <label>
                      {field.field_type}
                      <input
                        type={
                          ["email", "date"].includes(field.field_type)
                            ? field.field_type
                            : "text"
                        }
                        maxLength={2000}
                        value={answers[field.id]?.text || ""}
                        onChange={(e) =>
                          update({ id: field.id, text: e.target.value })
                        }
                      />
                    </label>
                  )}
                </fieldset>
              )}
            </aside>
            <section className="prepare-viewer">
              <Suspense fallback={<p>Loading PDF…</p>}>
                <PdfViewer
                  url="/api/signing/files/original"
                  session={session}
                  page={page}
                  onPage={setPage}
                  pages={doc.page_count}
                  overlay={(n) => (
                    <div className="field-overlay">
                      {doc.fields
                        .filter((f) => f.page_number === n)
                        .map((f) => (
                          <button
                            type="button"
                            key={f.id}
                            className={
                              "guest-box" +
                              (selected === f.id ? " selected" : "")
                            }
                            style={boxStyle(f)}
                            onClick={() => choose(f)}
                          >
                            {answers[f.id]?.text || f.field_type}
                            {f.required ? " *" : ""}
                          </button>
                        ))}
                    </div>
                  )}
                />
              </Suspense>
            </section>
          </div>
          <div className="layout-actions">
            <button
              disabled={busy || confirmDecline}
              onClick={() => void run(() => finish(false))}
            >
              Finish and sign
            </button>
            {confirmDecline ? (
              <>
                <p>Declining closes signing for everyone on this document.</p>
                <button
                  disabled={busy}
                  onClick={() => void run(() => finish(true))}
                >
                  Confirm decline
                </button>
                <button
                  disabled={busy}
                  onClick={() => setConfirmDecline(false)}
                >
                  Cancel
                </button>
              </>
            ) : (
              <button disabled={busy} onClick={() => setConfirmDecline(true)}>
                Decline
              </button>
            )}
          </div>
        </>
      )}
      {state === "signed" && <p role="status">Your signature was recorded.</p>}
      {state === "declined" && <p role="status">You declined this document.</p>}
      {notice && ["code", "verified"].includes(state) && (
        <p role="status">{notice}</p>
      )}
      {error && <p role="alert">{error}</p>}
    </main>
  );
}
