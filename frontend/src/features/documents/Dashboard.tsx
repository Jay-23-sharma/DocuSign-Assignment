import { useEffect, useState, type ReactNode, type FormEvent } from "react";

type Summary = { id: number; title: string; status: string; created_at: string; owned:boolean; sender:string; signed_count:number; recipient_count:number };
const statuses = ["draft", "sent", "viewed", "signed", "completed", "declined", "voided", "expired"];
const label = (value: string) => value.charAt(0).toUpperCase() + value.slice(1);

function UploadForm({ uploaded }: { uploaded: () => void }) {
  const [file, setFile] = useState<File | null>(null);
  const [title, setTitle] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!file || pending) return;
    const form = event.currentTarget;
    const data = new FormData();
    data.append("file", file);
    data.append("title", title);
    setPending(true);
    setError("");
    try {
      const response = await fetch("/api/documents", { method: "POST", body: data });
      if (response.status !== 201) {
        const body = await response.json().catch(() => null);
        setError(body?.error || "Upload failed. Please try again.");
        return;
      }
      form.reset();
      setFile(null);
      setTitle("");
      uploaded();
    } catch {
      setError("Could not confirm the upload. Check your documents before retrying.");
    } finally {
      setPending(false);
    }
  }

  return <section className="upload-panel">
    <h2>Upload a PDF</h2>
    <form onSubmit={submit}>
      <label htmlFor="pdf">PDF file</label>
      <input id="pdf" type="file" accept=".pdf,application/pdf" required disabled={pending}
        onChange={event => {
          const next = event.target.files?.[0] ?? null;
          setFile(next);
          setTitle(next ? next.name.replace(/\.pdf$/i, "").trim() || "Untitled document" : "");
          setError("");
        }} />
      <label htmlFor="document-title">Document title</label>
      <input id="document-title" value={title} disabled={pending}
        onChange={event => setTitle(event.target.value)} />
      {error && <p role="alert">{error}</p>}
      <button disabled={!file || pending}>{pending ? "Uploading…" : "Upload PDF"}</button>
    </form>
  </section>;
}

const views = { all: "All documents", sent: "Sent by me", received: "Received by me" } as const;
type View = keyof typeof views;

export function Dashboard({name, logoutControl}: {name: string; logoutControl: ReactNode}) {
  const [view, setView] = useState<View>("all");
  const [uploading, setUploading] = useState(false);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [revision, setRevision] = useState(0);
  const [list, setList] = useState<Summary[] | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setList(null);
      setFailed(false);
      try {
        const params = new URLSearchParams({ view, q: query, status });
        const response = await fetch("/api/documents?" + params, { signal: controller.signal });
        if (!response.ok) throw new Error("list unavailable");
        const data: Summary[] = await response.json();
        if (!controller.signal.aborted) setList(data);
      } catch {
        if (!controller.signal.aborted) setFailed(true);
      }
    }
    void load();
    return () => controller.abort();
  }, [view, query, status, revision]);

  return <main className="workspace dashboard">
    <aside className="dashboard-sidebar">
      <div className="dashboard-profile"><span aria-hidden="true">{Array.from(name)[0]?.toUpperCase()}</span><strong>{name}</strong></div>
      <nav aria-label="Document folders">
        {(Object.keys(views) as View[]).map(key => <button key={key} aria-current={view===key?"page":undefined} onClick={()=>{setList(null);setView(key);if(key===view)setRevision(n=>n+1)}}>{views[key]}</button>)}
      </nav>
      <div className="dashboard-account">{logoutControl}</div>
    </aside>
    <section className="document-list" aria-labelledby="documents-heading">
      <div className="document-heading"><h1 id="documents-heading">{views[view]}</h1>
        <button aria-expanded={uploading} onClick={()=>setUploading(v=>!v)}>{uploading?"Close upload":"Upload a PDF"}</button>
      </div>
      {uploading && <UploadForm uploaded={()=>{setUploading(false);setView("all");setQuery("");setStatus("");setRevision(n=>n+1)}}/>}
      <div className="document-filters">
        <label>Search by title<input type="search" value={query} onChange={e => setQuery(e.target.value)} /></label>
        <div><label htmlFor="document-status">Status</label><select id="document-status" value={status} onChange={e => setStatus(e.target.value)}>
          <option value="">All statuses</option>
          {statuses.map(s => <option key={s} value={s}>{label(s)}</option>)}
        </select></div>
      </div>
      {failed ? <div role="alert"><p>Documents could not be loaded.</p><button onClick={() => setRevision(n => n + 1)}>Retry</button></div>
        : list === null ? <p role="status">Loading documents…</p>
        : !list.length ? <p role="status">{query || status ? "No matching documents." : "No documents yet."}</p>
        : <div className="document-table"><table><thead><tr><th>Title</th><th>Sender</th><th>Status</th><th>Signatures</th><th>Upload date</th></tr></thead>
          <tbody>{list.map(doc => <tr key={doc.id}>
            <td>{doc.owned ? <a href={"/documents/" + doc.id}>{doc.title}</a> : <>{doc.title}{["sent","viewed"].includes(doc.status)&&<small className="invitation-note">Open your email invitation to sign.</small>}</>}</td>
            <td>{doc.owned?"You":doc.sender}</td>
            <td><span className="status-pill" data-status={doc.status}>{label(doc.status)}</span></td>
            <td>{doc.status==="draft"?"—":doc.signed_count+" of "+doc.recipient_count}</td>
            <td><time dateTime={doc.created_at}>{new Date(doc.created_at).toLocaleDateString()}</time></td>
          </tr>)}</tbody></table></div>}
    </section>
  </main>;
}
