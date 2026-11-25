import { useRef, useState, type PointerEvent } from "react";
type Signature = { text?: string; method?: string; reference?: string };
export function SignatureInput({
  value,
  onChange,
  upload,
  busy,
}: {
  value: Signature;
  onChange: (a: Signature) => void;
  upload: (method: string, file: Blob) => void;
  busy: boolean;
}) {
  const method = value.method || "typed";
  const canvas = useRef<HTMLCanvasElement>(null);
  const pointer = useRef<number | null>(null);
  const [ink, setInk] = useState(false);
  function point(e: PointerEvent<HTMLCanvasElement>) {
    const c = e.currentTarget,
      r = c.getBoundingClientRect();
    return [
      ((e.clientX - r.left) * c.width) / r.width,
      ((e.clientY - r.top) * c.height) / r.height,
    ] as const;
  }
  function start(e: PointerEvent<HTMLCanvasElement>) {
    if (busy || pointer.current !== null) return;
    const c = e.currentTarget,
      ctx = c.getContext("2d");
    if (!ctx) return;
    pointer.current = e.pointerId;
    c.setPointerCapture(e.pointerId);
    const [x, y] = point(e);
    ctx.lineWidth = 4;
    ctx.lineCap = "round";
    ctx.beginPath();
    ctx.moveTo(x, y);
    ctx.lineTo(x + 0.01, y);
    ctx.stroke();
    setInk(true);
    onChange({ method: "drawn" });
  }
  function move(e: PointerEvent<HTMLCanvasElement>) {
    if (pointer.current !== e.pointerId) return;
    const ctx = e.currentTarget.getContext("2d");
    if (ctx) {
      ctx.lineTo(...point(e));
      ctx.stroke();
    }
  }
  return (
    <>
      <label>
        Signature method{" "}
        <select
          value={method}
          onChange={(e) => {
            onChange({ method: e.target.value });
            setInk(false);
          }}
        >
          <option value="typed">Type</option>
          <option value="drawn">Draw</option>
          <option value="uploaded">Upload image</option>
        </select>
      </label>
      {method === "typed" && (
        <label>
          Type your signature{" "}
          <input
            value={value.text || ""}
            maxLength={2000}
            onChange={(e) => onChange({ method, text: e.target.value })}
          />
        </label>
      )}
      {method === "uploaded" && (
        <label>
          Signature image (PNG or JPEG){" "}
          <input
            type="file"
            accept="image/png,image/jpeg"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) {
                onChange({ method });
                upload(method, f);
              }
              e.target.value = "";
            }}
          />
        </label>
      )}
      {method === "drawn" && (
        <>
          <canvas
            aria-label="Draw your signature"
            width={600}
            height={200}
            ref={canvas}
            className="signature-pad"
            onPointerDown={start}
            onPointerMove={move}
            onLostPointerCapture={() => {
              pointer.current = null;
            }}
          />
          <button
            disabled={!ink || busy}
            onClick={() =>
              canvas.current?.toBlob((blob) => {
                if (blob) upload(method, blob);
              }, "image/png")
            }
          >
            Use drawing
          </button>
          <button
            onClick={() => {
              const c = canvas.current;
              c?.getContext("2d")?.clearRect(0, 0, c.width, c.height);
              onChange({ method });
              setInk(false);
            }}
          >
            Clear drawing
          </button>
        </>
      )}
      {value.reference && <p>Signature image ready.</p>}
    </>
  );
}
