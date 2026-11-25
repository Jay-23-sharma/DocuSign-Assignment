import { lazy, Suspense, useEffect, useState } from "react";
import { moveBox } from "../../components/pdf/fieldGeometry";
import { FieldOverlay } from "./FieldOverlay";
import { Recipients } from "./Recipients";
import type { Box, PrepareDetail, Recipient } from "./types";

const fieldTypes = [
  "signature",
  "initials",
  "name",
  "date",
  "email",
  "text",
  "checkbox",
] as const;
const PdfViewer = lazy(() => import("../../components/pdf/PdfViewer"));
export function Prepare({
  id,
  doc,
  onDirty,
}: {
  id: string;
  doc: PrepareDetail;
  onDirty: (dirty: boolean) => void;
}) {
  const base = "/api/documents/" + id;
  const [people, setPeople] = useState(doc.recipients);
  const [person, setPerson] = useState(doc.recipients[0]?.id ?? 0);
  const [baseline, setBaseline] = useState<Box[]>(() =>
    doc.fields.map((f) => ({ ...f, key: crypto.randomUUID() })),
  );
  const [boxes, setBoxes] = useState<Box[]>(baseline);
  const dirty = JSON.stringify(boxes) !== JSON.stringify(baseline);
  const [saveError, setSaveError] = useState("");
  useEffect(() => {
    onDirty(dirty);
    if (!dirty) return;
    function warn(event: BeforeUnloadEvent) {
      event.preventDefault();
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", warn);
    return () => {
      window.removeEventListener("beforeunload", warn);
      onDirty(false);
    };
  }, [dirty, onDirty]);
  const [fieldType, setFieldType] = useState("signature");
  const [selected, select] = useState<string | null>(null);
  const current = boxes.find((box) => box.key === selected);
  function updateSelected(
    changes: Partial<Pick<Box, "recipient_id" | "required">>,
  ) {
    setBoxes((list) =>
      list.map((box) => (box.key === selected ? { ...box, ...changes } : box)),
    );
  }
  const [busy, setBusy] = useState(false);

  function savedRecipient(value: Recipient) {
    setPeople((list) =>
      list.some((p) => p.id === value.id)
        ? list.map((p) => (p.id === value.id ? value : p))
        : [...list, value],
    );
    if (!person) setPerson(value.id);
  }
  function deletedRecipient(rid: number) {
    setPeople((list) => list.filter((p) => p.id !== rid));
    if (person === rid) setPerson(people.find((p) => p.id !== rid)?.id ?? 0);
    setBoxes((list) => list.filter((b) => b.recipient_id !== rid));
    setBaseline((list) => list.filter((b) => b.recipient_id !== rid));
    if (boxes.some((b) => b.key === selected && b.recipient_id === rid))
      select(null);
  }

  const [openReview, setOpenReview] = useState(false);
  useEffect(() => {
    if (openReview && !dirty) window.location.assign("/documents/" + id + "/review");
  }, [openReview, dirty, id]);

  async function saveLayout() {
    setBusy(true);
    setSaveError("");
    try {
      const fields = boxes.map(({ key: _key, ...field }) => field);
      const response = await fetch(base + "/fields", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ fields }),
      });
      if (!response.ok)
        throw new Error(
          (await response.json()).error || "Layout could not be saved.",
        );
      setBaseline(boxes);
      return true;
    } catch (e) {
      setSaveError(
        e instanceof Error ? e.message : "Layout could not be saved.",
      );
      return false;
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="prepare">
      <a href={"/documents/" + id}>Close editor</a>
      <h2>Prepare {doc.title}</h2>
      <div className="layout-actions">
        <button disabled={busy} onClick={saveLayout}>
          Save layout
        </button>
        <button disabled={busy} onClick={async () => {
          if (!dirty || await saveLayout()) setOpenReview(true);
        }}>Review and send</button>
        <span role="status">
          {dirty ? "Unsaved layout edits" : "Layout saved"}
        </span>
      </div>
      {saveError && <p role="alert">{saveError}</p>}
      <div className="prepare-grid">
        <aside>
          <Recipients
            base={base}
            recipients={people}
            busy={busy}
            setBusy={setBusy}
            onSave={savedRecipient}
            onDelete={deletedRecipient}
            deletionMessage={(rid, count) => {
              const unsaved = boxes.filter(
                (b) =>
                  b.recipient_id === rid &&
                  !baseline.some(
                    (saved) => JSON.stringify(saved) === JSON.stringify(b),
                  ),
              ).length;
              return (
                count +
                (count === 1 ? " saved field" : " saved fields") +
                " will be deleted. " +
                unsaved +
                (unsaved === 1 ? " unsaved box" : " unsaved boxes") +
                " for this recipient will also be discarded."
              );
            }}
          />
          <h3>Place a field</h3>
          {people.length ? (
            <label>
              Placement recipient
              <select
                aria-label="Placement recipient"
                value={person}
                onChange={(e) => setPerson(Number(e.target.value))}
              >
                {people.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <p>Add a recipient before placing a field.</p>
          )}
          {people.length > 0 && (
            <>
              <label>
                Field type
                <select
                  aria-label="Field type"
                  value={fieldType}
                  onChange={(e) => setFieldType(e.target.value)}
                >
                  {fieldTypes.map((type) => (
                    <option key={type} value={type}>
                      {type}
                    </option>
                  ))}
                </select>
              </label>
              <p>
                Click the page to place a field. Drag the box to move it or its
                corner to resize.
              </p>
            </>
          )}
          {current && (
            <fieldset disabled={busy}>
              <legend>Selected field</legend>
              <p>
                {current.field_type} · Page {current.page_number}
              </p>
              <label>
                Assigned recipient
                <select
                  aria-label="Assigned recipient"
                  value={current.recipient_id}
                  onChange={(e) =>
                    updateSelected({ recipient_id: Number(e.target.value) })
                  }
                >
                  {people.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="required-setting">
                <input
                  type="checkbox"
                  checked={current.required}
                  onChange={(e) =>
                    updateSelected({ required: e.target.checked })
                  }
                />{" "}
                Required
              </label>
              <button
                disabled={busy}
                onClick={() => {
                  setBoxes((list) => list.filter((b) => b.key !== selected));
                  select(null);
                }}
              >
                Delete field
              </button>
            </fieldset>
          )}
        </aside>
        <div className="prepare-viewer">
          <Suspense fallback={<p>Loading viewer…</p>}>
            <PdfViewer
              url={base + "/files/original"}
              pages={doc.original!.page_count}
              overlay={(page) => (
                <FieldOverlay
                  boxes={boxes.filter((b) => b.page_number === page)}
                  recipients={people}
                  selected={selected}
                  select={select}
                  disabled={busy || !person}
                  add={(x, y) => {
                    const key = crypto.randomUUID();
                    setBoxes((list) => [
                      ...list,
                      {
                        key,
                        recipient_id: person,
                        field_type: fieldType,
                        required: true,
                        page_number: page,
                        ...moveBox(
                          { x: 0, y: 0, width: 0.22, height: 0.06 },
                          x,
                          y,
                        ),
                      },
                    ]);
                    select(key);
                  }}
                  change={(key, rect) =>
                    setBoxes((list) =>
                      list.map((b) => (b.key === key ? { ...b, ...rect } : b)),
                    )
                  }
                />
              )}
            />
          </Suspense>
        </div>
      </div>
    </section>
  );
}
