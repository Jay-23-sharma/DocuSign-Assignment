import { useEffect, useRef, useState, type ReactNode } from "react";
import { getDocument, GlobalWorkerOptions, type PDFDocumentProxy, type RenderTask } from "pdfjs-dist";
import workerURL from "pdfjs-dist/build/pdf.worker.min.mjs?url";

GlobalWorkerOptions.workerSrc = workerURL;

function PageCanvas({ pdf, number, overlay }: { pdf: PDFDocumentProxy; number: number; overlay?: ReactNode }) {
  const ref = useRef<HTMLCanvasElement>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  useEffect(() => {
    let disposed = false;
    let task: RenderTask | undefined;
    async function draw() {
      try {
        const page = await pdf.getPage(number);
        const canvas = ref.current;
        if (disposed || !canvas) return;
        const size = page.getViewport({ scale: 1 });
        const scale = Math.min(2, 1600 / size.width, 2400 / size.height);
        const viewport = page.getViewport({ scale });
        canvas.width = Math.ceil(viewport.width);
        canvas.height = Math.ceil(viewport.height);
        task = page.render({ canvas, viewport });
        await task.promise;
        if (!disposed) setState("ready");
      } catch {
        if (!disposed) setState("error");
      }
    }
    void draw();
    return () => { disposed = true; task?.cancel(); };
  }, [pdf, number]);
  return <>
    {state === "error" && <p role="alert">Could not render this page.</p>}
    {state === "loading" && <p role="status">Rendering page…</p>}
    <div className="pdf-page" hidden={state !== "ready"}><canvas ref={ref} aria-label={"PDF page " + number} />
      {state === "ready" && overlay}
    </div>
  </>;
}

export default function PdfViewer({ url, pages, overlay, session, page, onPage }: { url: string; pages: number; session?:number; page?:number; onPage?:(page:number)=>void; overlay?: (page: number) => ReactNode }) {
  const [pdf, setPdf] = useState<PDFDocumentProxy | null>(null);
  const [localPage, setLocalPage] = useState(1);
  const number=page ?? localPage;
  const setNumber=onPage ?? setLocalPage;
  const [zoom, setZoom] = useState(100);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let disposed = false;
    const task = getDocument({ url, withCredentials: true, httpHeaders:session?{"X-Signing-Session":String(session)}:undefined,
      cMapUrl: "/pdf-assets/cmaps/", standardFontDataUrl: "/pdf-assets/standard_fonts/",
      wasmUrl: "/pdf-assets/wasm/", iccUrl: "/pdf-assets/iccs/" });
    task.promise.then(result => { if (!disposed) setPdf(result); })
      .catch(() => { if (!disposed) setFailed(true); });
    return () => { disposed = true; void task.destroy(); };
  }, [url,session]);
  const count = pdf?.numPages ?? pages;
  return <>
    <nav className="page-controls" aria-label="PDF pages">
      <button disabled={!pdf || number === 1} onClick={() => setNumber(number - 1)}>Previous page</button>
      <span aria-live="polite">Page {number} of {count}</span>
      <button disabled={!pdf || number === count} onClick={() => setNumber(number + 1)}>Next page</button>
      {overlay && <label>Zoom <select aria-label="Zoom" value={zoom} onChange={e => setZoom(Number(e.target.value))}>{[75,100,125,150,200].map(z => <option key={z} value={z}>{z}%</option>)}</select></label>}
    </nav>
    {failed ? <p role="alert">Could not load the original PDF.</p>
      : pdf ? <div className="pdf-scroll"><div style={{ width: zoom + "%" }}><PageCanvas key={number} number={number} pdf={pdf} overlay={overlay?.(number)} /></div></div> : <p role="status">Loading PDF…</p>}
  </>;
}
