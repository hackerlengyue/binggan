import test from "node:test";
import assert from "node:assert/strict";
import { highlightJson } from "../src/lib/json-highlight";

test("JSON highlighting preserves formatted text and classifies escaped strings", () => {
  const value = {
    'escaped " key': 'quote: " slash: \\ newline: \n',
    nested: { 中文: "true: 42 null" },
  };
  const result = highlightJson(JSON.stringify(value));
  assert.equal(result.text, JSON.stringify(value, null, 2));
  assert.equal(result.tokens!.map((token) => token.text).join(""), result.text);
  assert.deepEqual(
    result.tokens!.filter((token) => token.kind !== "plain"),
    [
      { kind: "key", text: JSON.stringify('escaped " key') },
      { kind: "string", text: JSON.stringify(value['escaped " key']) },
      { kind: "key", text: '"nested"' },
      { kind: "key", text: '"中文"' },
      { kind: "string", text: '"true: 42 null"' },
    ],
  );
});

test("numbers, booleans and null receive distinct JSON token kinds", () => {
  const result = highlightJson("[0, -4.25, 1e30, 1e-8, true, false, null]");
  assert.deepEqual(
    result.tokens!.filter((token) => token.kind !== "plain"),
    [
      { kind: "number", text: "0" },
      { kind: "number", text: "-4.25" },
      { kind: "number", text: "1e+30" },
      { kind: "number", text: "1e-8" },
      { kind: "boolean", text: "true" },
      { kind: "boolean", text: "false" },
      { kind: "null", text: "null" },
    ],
  );
  assert.equal(result.tokens!.map((token) => token.text).join(""), result.text);
});

test("non-JSON and truncated responses remain unchanged plain text", () => {
  for (const body of ["", "plain text: 42", '{"unfinished":', "<p>HTML</p>"]) {
    assert.deepEqual(highlightJson(body), { text: body, tokens: null });
  }
});

test("HTML-like JSON strings stay literal token text", () => {
  const html = '<script>alert("x")</script><img src=x onerror=alert(1)>&';
  const result = highlightJson(JSON.stringify({ html }));
  assert.deepEqual(
    result.tokens!.filter((token) => token.kind === "string"),
    [{ kind: "string", text: JSON.stringify(html) }],
  );
  assert.equal(result.tokens!.map((token) => token.text).join(""), result.text);
  assert.deepEqual(JSON.parse(result.text), { html });
});

test("large JSON keeps formatting while falling back to one plain text body", () => {
  for (const value of [{ body: "x".repeat(200_000) }, Array(6000).fill(1)]) {
    assert.deepEqual(highlightJson(JSON.stringify(value)), {
      text: JSON.stringify(value, null, 2),
      tokens: null,
    });
  }
});
