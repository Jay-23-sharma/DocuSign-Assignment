import test from "node:test";
import assert from "node:assert/strict";
import {
  moveBox,
  resizeBox,
  pagePoint,
  boxStyle,
} from "../src/components/pdf/fieldGeometry.ts";

test("top-left page coordinates survive offsets, scroll, zoom and landscape orientation", () => {
  for (const rect of [
    { left: 205, top: 80, width: 420, height: 680 },
    { left: -80, top: -320, width: 1050, height: 700 },
  ]) {
    const p = pagePoint(
      rect.left + rect.width * 0.35,
      rect.top + rect.height * 0.25,
      rect,
    );
    const box = moveBox({ x: 0, y: 0, width: 0.2, height: 0.1 }, p.x, p.y);
    assert.deepEqual(boxStyle(box), {
      left: "35%",
      top: "25%",
      width: "20%",
      height: "10%",
    });
  }
});

test("placing, moving and resizing at page edges produce positive, bounded decimal rectangles", () => {
  const b = moveBox({ x: 0, y: 0, width: 0.123456, height: 0.234567 }, 1, 1);
  assert.deepEqual([b.x, b.y], [0.876544, 0.765433]);
  assert.deepEqual(resizeBox(b, 2, 2), b);
  assert.deepEqual(moveBox(b, -1, -2), { ...b, x: 0, y: 0 });
  const small = resizeBox(b, -1, 0);
  assert.ok(small.width > 0 && small.height > 0);
});
