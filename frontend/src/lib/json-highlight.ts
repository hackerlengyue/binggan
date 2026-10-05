export type JsonTokenKind =
  "key" | "string" | "number" | "boolean" | "null" | "plain";

export interface JsonToken {
  kind: JsonTokenKind;
  text: string;
}

export interface HighlightedJson {
  text: string;
  tokens: JsonToken[] | null;
}

const MAX_HIGHLIGHT_LENGTH = 200_000;
const MAX_HIGHLIGHT_TOKENS = 10_000;

export function highlightJson(body: string): HighlightedJson {
  let text: string;
  try {
    text = JSON.stringify(JSON.parse(body), null, 2);
  } catch {
    return { text: body, tokens: null };
  }
  // Keep large responses readable without creating thousands of DOM nodes.
  if (text.length > MAX_HIGHLIGHT_LENGTH) return { text, tokens: null };

  const tokens: JsonToken[] = [];
  const pattern =
    /"(?:\\.|[^"\\])*"|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?|\b(?:true|false|null)\b/g;
  let offset = 0;
  for (const match of text.matchAll(pattern)) {
    if (match.index > offset)
      tokens.push({ kind: "plain", text: text.slice(offset, match.index) });
    const value = match[0];
    const end = match.index + value.length;
    const kind: JsonTokenKind = value.startsWith('"')
      ? text[end] === ":"
        ? "key"
        : "string"
      : value === "null"
        ? "null"
        : value === "true" || value === "false"
          ? "boolean"
          : "number";
    tokens.push({ kind, text: value });
    if (tokens.length >= MAX_HIGHLIGHT_TOKENS) return { text, tokens: null };
    offset = end;
  }
  if (offset < text.length)
    tokens.push({ kind: "plain", text: text.slice(offset) });
  return { text, tokens };
}
