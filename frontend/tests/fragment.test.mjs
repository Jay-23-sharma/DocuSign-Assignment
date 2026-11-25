import test from "node:test";
import assert from "node:assert/strict";
import { takeToken } from "../src/app/verificationToken.ts";

test("captures the exact token and strips only the fragment", () => {
  const changes = [];
  const location = new URL("http://localhost/verify-email?lang=en#test-_secret");
  const history = {state: null, replaceState(...args) { changes.push(args); }};
  assert.equal(takeToken({location, history}), "test-_secret");
  assert.deepEqual(changes, [[null, "", "/verify-email?lang=en"]]);
});
