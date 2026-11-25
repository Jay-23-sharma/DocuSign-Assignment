import { useRef, type PointerEvent } from "react";
import {
  boxStyle,
  moveBox,
  pagePoint,
  resizeBox,
  type Rect,
} from "../../components/pdf/fieldGeometry";
import type { Box, Recipient } from "./types";

type Props = {
  boxes: Box[];
  recipients: Recipient[];
  selected: string | null;
  disabled: boolean;
  select: (key: string) => void;
  add: (x: number, y: number) => void;
  change: (key: string, rect: Rect) => void;
};
export function FieldOverlay({
  boxes,
  recipients,
  selected,
  disabled,
  select,
  add,
  change,
}: Props) {
  const layer = useRef<HTMLDivElement>(null);
  const drag = useRef<{
    box: Box;
    x: number;
    y: number;
    resize: boolean;
    pointer: number;
  } | null>(null);
  function point(e: PointerEvent) {
    return pagePoint(
      e.clientX,
      e.clientY,
      layer.current!.getBoundingClientRect(),
    );
  }
  function begin(e: PointerEvent<HTMLDivElement>, box: Box) {
    if (disabled || e.button !== 0 || !e.isPrimary) return;
    e.stopPropagation();
    e.preventDefault();
    select(box.key);
    drag.current = {
      box,
      ...point(e),
      resize: (e.target as HTMLElement).classList.contains("resize-handle"),
      pointer: e.pointerId,
    };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function move(e: PointerEvent) {
    const start = drag.current;
    if (disabled || !start || start.pointer !== e.pointerId) return;
    const p = point(e),
      dx = p.x - start.x,
      dy = p.y - start.y;
    const b = start.box;
    change(
      b.key,
      start.resize
        ? resizeBox(b, b.width + dx, b.height + dy)
        : moveBox(b, b.x + dx, b.y + dy),
    );
  }
  return (
    <div
      className="field-overlay"
      ref={layer}
      onPointerDown={(e) => {
        if (
          disabled ||
          e.target !== e.currentTarget ||
          e.button !== 0 ||
          !e.isPrimary
        )
          return;
        const p = point(e);
        add(p.x, p.y);
      }}
    >
      {boxes.map((box) => (
        <div
          key={box.key}
          className={"field-box" + (selected === box.key ? " selected" : "")}
          style={boxStyle(box)}
          tabIndex={0}
          role="button"
          title={
            recipients.find((p) => p.id === box.recipient_id)?.name +
            " · " +
            box.field_type
          }
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              select(box.key);
            }
          }}
          onFocus={() => select(box.key)}
          onPointerDown={(e) => begin(e, box)}
          onPointerMove={move}
          onLostPointerCapture={() => {
            drag.current = null;
          }}
        >
          <span>
            {recipients.find((p) => p.id === box.recipient_id)?.name} ·{" "}
            {box.field_type}
          </span>
          <span className="resize-handle" title="Resize field" />
        </div>
      ))}
    </div>
  );
}
