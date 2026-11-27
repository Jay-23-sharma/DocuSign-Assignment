import { lazy, Suspense, useRef, useState } from "react";
import { boxStyle } from "../../components/pdf/fieldGeometry";
import type { PrepareDetail, Recipient } from "./types";

const PdfViewer = lazy(() => import("../../components/pdf/PdfViewer"));

export function RecipientPreview({ id, doc, recipient, switchTo, back }: {
  id: string;
  doc: PrepareDetail;
  recipient: Recipient;
  switchTo: (id: number) => void;
  back: () => void;
}) {
  const [page, setPage] = useState(1);
  const [selected, select] = useState<number | null>(null);
  const highlighted = useRef<HTMLSpanElement | null>(null);
  const fields = doc.fields.filter(f => f.recipient_id === recipient.id)
    .sort((a, b) => a.page_number - b.page_number || a.y - b.y || a.x - b.x);

  function choose(index: number) {
    if (selected === index) scrollToField(highlighted.current);
    setPage(fields[index].page_number);
    select(index);
  }
  function scrollToField(node: HTMLSpanElement | null) {
    highlighted.current = node;
    node?.scrollIntoView({ block: "center", inline: "nearest" });
  }
  return <section>
    <div className="layout-actions">
      <button onClick={back}>Back to review</button>
      <a href={"/documents/" + id + "/prepare"}>Edit fields</a>
    </div>
    <h2>Sender preview · {recipient.name}, {recipient.email}</h2>
    <p>{doc.title} · Read-only</p>
    <label htmlFor="preview-recipient">Preview as</label>{" "}
      <select id="preview-recipient" value={recipient.id} onChange={e => switchTo(Number(e.target.value))}>
        {doc.recipients.map(person => <option key={person.id} value={person.id}>
          {person.name} · {person.email}
        </option>)}
      </select>
    {fields.length ? <>
      <p>{fields.length} assigned {fields.length === 1 ? "field" : "fields"}</p>
      <ol aria-label="Assigned fields">
        {fields.map((field, i) => <li key={i}>
          <button onClick={() => choose(i)} aria-pressed={selected === i}>
            Page {field.page_number} · {field.field_type} · {field.required ? "Required" : "Optional"}
          </button>
        </li>)}
      </ol>
    </> : <p>{recipient.name} has no fields assigned.</p>}
    {doc.original ? <Suspense fallback={<p>Loading PDF…</p>}>
      <PdfViewer url={"/api/documents/" + id + "/files/original"} pages={doc.original.page_count}
        page={page} onPage={next => { select(null); setPage(next); }}
        overlay={number => fields.map((field, i) => field.page_number === number &&
          <span key={i} ref={selected === i ? scrollToField : undefined}
            className={"guest-box recipient-preview-box" + (selected === i ? " selected" : "")}
            style={boxStyle(field)}>
            {field.field_type === "checkbox" && "☐ "}{field.field_type} · {field.required ? "Required" : "Optional"}
          </span>)} />
    </Suspense> : <p>No original PDF is available.</p>}
  </section>;
}
