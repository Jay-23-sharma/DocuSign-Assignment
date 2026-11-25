export type Rect = { x: number; y: number; width: number; height: number };
type Bounds = { left: number; top: number; width: number; height: number };
const clamp = (v: number, lo: number, hi: number) =>
  Math.max(lo, Math.min(v, hi));
// Quantize independently of viewport pixels. PostgreSQL numeric sums decimal fractions exactly.
const units = 1_000_000;
const tick = (v: number) => Math.round(v * units);
export function pagePoint(clientX: number, clientY: number, page: Bounds) {
  return {
    x: (clientX - page.left) / page.width,
    y: (clientY - page.top) / page.height,
  };
}
export function moveBox(box: Rect, x: number, y: number): Rect {
  return {
    ...box,
    x: clamp(tick(x), 0, units - tick(box.width)) / units,
    y: clamp(tick(y), 0, units - tick(box.height)) / units,
  };
}
export function resizeBox(box: Rect, width: number, height: number): Rect {
  return {
    ...box,
    width: clamp(tick(width), 1, units - tick(box.x)) / units,
    height: clamp(tick(height), 1, units - tick(box.y)) / units,
  };
}
export function boxStyle(box: Rect) {
  return {
    left: box.x * 100 + "%",
    top: box.y * 100 + "%",
    width: box.width * 100 + "%",
    height: box.height * 100 + "%",
  };
}
