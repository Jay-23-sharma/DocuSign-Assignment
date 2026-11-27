import { lazy, Suspense, useEffect, useState } from "react";

import { History } from "./History";
import { DeliveryResults } from "./DeliveryResults";
import { Review } from "../prepare/Review";
import { Prepare } from "../prepare/Prepare";
import type { PrepareDetail } from "../prepare/types";

const PdfViewer = lazy(() => import("../../components/pdf/PdfViewer"));

export function Preview({
  id,
  prepare,
  review,
  onDirty,
}: {
  id: string;
  prepare?: boolean;
  review?: boolean;
  onDirty: (value: boolean) => void;
}) {
  const [doc, setDoc] = useState<PrepareDetail | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      try {
        const response = await fetch("/api/documents/" + id, {
          signal: controller.signal,
        });
        if (response.status === 404) {
          setError("Document not found.");
          return;
        }
        if (!response.ok) throw new Error("preview unavailable");
        const data = await response.json();
        if (!controller.signal.aborted) setDoc(data);
      } catch {
        if (!controller.signal.aborted)
          setError("Could not load this document.");
      }
    }
    void load();
    return () => controller.abort();
  }, [id]);

  const [pending, setPending] = useState(false);
  const [completionError, setCompletionError] = useState("");
  async function complete() {
    setPending(true);
    setCompletionError("");
    try {
      const response = await fetch("/api/documents/" + id + "/complete", {
        method: "POST",
      });
      if (!response.ok) throw new Error();
      const latest = await fetch("/api/documents/" + id);
      if (!latest.ok) throw new Error();
      setDoc(await latest.json());
    } catch {
      setCompletionError(
        "Could not confirm the completed PDF. Retry generation or refresh this page.",
      );
    } finally {
      setPending(false);
    }
  }
  const [confirmVoid, setConfirmVoid] = useState(false);
  async function voidRequest() {
    setPending(true);
    setCompletionError("");
    try {
      const response = await fetch("/api/documents/" + id + "/void", {
        method: "POST",
      });
      if (!response.ok) throw new Error((await response.json()).error);
      const latest = await fetch("/api/documents/" + id);
      if (!latest.ok) throw new Error("Refresh to check the outcome.");
      setDoc(await latest.json());
      setConfirmVoid(false);
    } catch (e) {
      setCompletionError(
        e instanceof Error ? e.message : "Refresh to check the outcome.",
      );
    } finally {
      setPending(false);
    }
  }
  const completed = doc?.status === "completed";
  const [showOriginal, setShowOriginal] = useState(false);
  const fileURL =
    "/api/documents/" +
    id +
    "/files/" +
    (completed && !showOriginal ? "completed" : "original");
  const editable = doc?.status === "draft" && !!doc.original;
  if (doc && review && doc.status === "draft")
    return <Review id={id} doc={doc} />;
  if (doc && prepare && editable)
    return <Prepare id={id} doc={doc} onDirty={onDirty} />;
  return (
    <section className="preview">
      <a className="document-action-link" href="/">
        Back to documents
      </a>
      {error ? (
        <p role="alert">{error}</p>
      ) : !doc ? (
        <p role="status">Loading document…</p>
      ) : (
        <>
          <h2>{doc.title}</h2>
          <p>
            Status:{" "}
            <strong>{doc.status[0].toUpperCase() + doc.status.slice(1)}</strong>
          </p>
          {["sent", "viewed"].includes(doc.status) && (
            <section>
              {confirmVoid ? (
                <>
                  <p>
                    Void this request? Outstanding recipients will lose signing
                    access. Previously accepted signatures will be kept.
                  </p>
                  <button disabled={pending} onClick={() => void voidRequest()}>
                    Confirm void
                  </button>{" "}
                  <button
                    disabled={pending}
                    onClick={() => setConfirmVoid(false)}
                  >
                    Cancel
                  </button>
                </>
              ) : (
                <button onClick={() => setConfirmVoid(true)}>
                  Void request
                </button>
              )}
            </section>
          )}
          {completionError && <p role="alert">{completionError}</p>}
          {doc.status !== "draft" && (
            <DeliveryResults id={id} doc={doc} onRefresh={setDoc} />
          )}
          {editable && <a className="document-action-link" href={"/documents/" + id + "/prepare"}>Prepare</a>}
          {doc.status === "signed" && (
            <section>
              <p>All recipients have signed. The completed PDF is pending.</p>
              <button disabled={pending} onClick={() => void complete()}>
                {pending ? "Generating…" : "Generate completed PDF"}
              </button>
            </section>
          )}
          {completed && (
            <section>
              <h3>Completed PDF</h3>
              <div className="pdf-actions">
                <a
                  href={"/api/documents/" + id + "/files/completed?download=1"}
                  download
                >
                  Download completed PDF
                </a>
                <button onClick={() => setShowOriginal((v) => !v)}>
                  {showOriginal
                    ? "Preview completed PDF"
                    : "Preview original PDF"}
                </button>
              </div>
              <p>
                SHA-256{" "}
                <code style={{ overflowWrap: "anywhere" }}>
                  {doc.completed_sha256}
                </code>
              </p>
            </section>
          )}
          <History id={id} doc={doc} />
          {!doc.original ? (
            <p>This draft has no original PDF.</p>
          ) : (
            <Suspense fallback={<p>Loading viewer…</p>}>
              <PdfViewer
                key={fileURL}
                url={fileURL}
                pages={doc.original.page_count}
              />
            </Suspense>
          )}
        </>
      )}
    </section>
  );
}
