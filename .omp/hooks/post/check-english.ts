import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { registerPostCheck, runCheckCli, type PostCheck } from "../lib/hook-post-check.ts";
import { readText, walkFiles } from "../lib/hook-repo.ts";

/**
 * Check-English Post Hook
 *
 * Documentation and code comments are written in English (rule://english).
 * Text is non-English when it has a letter from a script other than Latin
 * (Hangul, Kana, Han, Cyrillic, …); symbols, punctuation and accented Latin
 * letters pass. Proper nouns that must keep their own script are listed under
 * "Allowed words" in .omp/rules/english.md and pass anywhere.
 *
 *   Markdown       every line, except fenced code blocks and `code spans`
 *   Go, TS         // and block comments; string, raw-string, template and
 *                  regex literals are code (test data, matched user words)
 *   shell, ps1,    # comments (and PowerShell <# #> blocks); quoted text is code
 *   YAML, TOML,
 *   Makefile, tape
 *   LICENSE        every line
 */

type Kind = "markdown" | "slash" | "hash" | "prose";

/** File kind by name; null for files that are not checked (binaries, go.sum, …). */
function kindOf(path: string): Kind | null {
  const base = path.split("/").pop() ?? "";
  if (base === "Makefile" || base === ".gitignore" || base === ".gitmodules") return "hash";
  if (base === "LICENSE") return "prose";
  const ext = /\.([\w]+)$/.exec(base)?.[1] ?? "";
  switch (ext) {
    case "md":
      return "markdown";
    case "go":
    case "ts":
    case "mod":
      return "slash";
    case "sh":
    case "zsh":
    case "bash":
    case "ps1":
    case "yml":
    case "yaml":
    case "toml":
    case "tape":
      return "hash";
  }
  return null;
}

/** A line of checked text: its 1-based line number and the text. */
type Piece = [number, string];

/** Markdown outside fenced code blocks and inline code spans. */
function markdownText(source: string): Piece[] {
  const pieces: Piece[] = [];
  let fence: string | null = null;
  source.split("\n").forEach((line, i) => {
    const marker = /^\s*(`{3,}|~{3,})/.exec(line)?.[1];
    if (marker !== undefined && (fence === null || marker.startsWith(fence))) {
      fence = fence === null ? marker : null;
      return;
    }
    if (fence !== null) return;
    pieces.push([i + 1, line.replace(/(`+)[\s\S]*?\1/g, " ")]);
  });
  return pieces;
}

/**
 * Comments of a C-like source (Go, TypeScript). Literals are skipped: "…" and
 * '…' end at the line; `…` (Go raw string, TS template) may span lines. In
 * TypeScript a `/` where an expression starts begins a regex literal.
 */
function slashComments(source: string, regexLiterals: boolean): Piece[] {
  const pieces: Piece[] = [];
  let line = 1;
  let i = 0;
  let block: { start: number; text: string } | null = null;
  let lastCode = ""; // last non-space code character, to tell a regex from division
  const push = (at: number, text: string) => {
    text.split("\n").forEach((part, k) => pieces.push([at + k, part]));
  };
  while (i < source.length) {
    const c = source[i];
    if (block !== null) {
      if (source.startsWith("*/", i)) {
        push(block.start, block.text);
        block = null;
        i += 2;
        continue;
      }
      if (c === "\n") line += 1;
      block.text += c;
      i += 1;
      continue;
    }
    if (c === "\n") {
      line += 1;
      i += 1;
      continue;
    }
    if (source.startsWith("//", i)) {
      const end = source.indexOf("\n", i);
      push(line, source.slice(i + 2, end === -1 ? undefined : end));
      i = end === -1 ? source.length : end;
      continue;
    }
    if (source.startsWith("/*", i)) {
      block = { start: line, text: "" };
      i += 2;
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      i += 1;
      while (i < source.length && source[i] !== c) {
        if (source[i] === "\\" && c !== "`") i += 1;
        else if (source[i] === "\n") {
          if (c !== "`") break; // unterminated: stop at the line
          line += 1;
        }
        i += 1;
      }
      i += 1;
      lastCode = c;
      continue;
    }
    if (regexLiterals && c === "/" && (lastCode === "" || "(,=:[!&|?{};+-*%<>~^".includes(lastCode))) {
      let inClass = false;
      i += 1;
      while (i < source.length && source[i] !== "\n") {
        const d = source[i];
        if (d === "\\") i += 1;
        else if (d === "[") inClass = true;
        else if (d === "]") inClass = false;
        else if (d === "/" && !inClass) break;
        i += 1;
      }
      i += 1;
      lastCode = "/";
      continue;
    }
    if (!/\s/.test(c)) lastCode = c;
    i += 1;
  }
  if (block !== null) push(block.start, block.text);
  return pieces;
}

/** `#` comments (shell, PowerShell, YAML, TOML, make), plus PowerShell `<# … #>` blocks. */
function hashComments(source: string): Piece[] {
  const pieces: Piece[] = [];
  let inBlock = false;
  source.split("\n").forEach((text, index) => {
    const n = index + 1;
    let quote: string | null = null;
    for (let i = 0; i < text.length; i += 1) {
      const c = text[i];
      if (inBlock) {
        const end = text.indexOf("#>", i);
        pieces.push([n, text.slice(i, end === -1 ? undefined : end)]);
        if (end === -1) return;
        inBlock = false;
        i = end + 1;
        continue;
      }
      if (quote !== null) {
        if (c === "\\" && quote === '"') i += 1;
        else if (c === quote) quote = null;
        continue;
      }
      if (c === "'" || c === '"') {
        quote = c;
        continue;
      }
      if (text.startsWith("<#", i)) {
        inBlock = true;
        i += 1;
        continue;
      }
      // A comment starts a word: not `$#`, `${#x}`, `a#b`.
      if (c === "#" && (i === 0 || /[\s;]/.test(text[i - 1]))) {
        pieces.push([n, text.slice(i + 1)]);
        return;
      }
    }
  });
  return pieces;
}

/** The proper nouns english.md allows: `- \`word\`` items under "## Allowed words". */
export function allowedWords(rule: string): string[] {
  const section = /^## Allowed words\s*\n([\s\S]*?)(?=^## |(?![\s\S]))/m.exec(rule)?.[1] ?? "";
  return [...section.matchAll(/^-\s+`([^`]+)`/gm)].map((m) => m[1]);
}

const NON_LATIN = /[^\P{L}\p{Script=Latin}]+/gu;

/** Issues for one file's checked text. */
export function scanFile(path: string, source: string, allowed: string[]): string[] {
  const kind = kindOf(path);
  if (kind === null) return [];
  const pieces =
    kind === "markdown" ? markdownText(source)
    : kind === "slash" ? slashComments(source, path.endsWith(".ts"))
    : kind === "hash" ? hashComments(source)
    : source.split("\n").map((text, i): Piece => [i + 1, text]);
  const issues: string[] = [];
  for (const [n, raw] of pieces) {
    let text = raw;
    for (const word of allowed) text = text.split(word).join(" ");
    const found = text.match(NON_LATIN);
    if (found === null) continue;
    const where = kind === "slash" || kind === "hash" ? "comment" : "text";
    issues.push(`${path}:${n}: non-English ${where} (${[...new Set(found)].slice(0, 3).join(", ")})`);
  }
  return issues;
}

export function checkEnglish(): string[] {
  const rule = readText(".omp/rules/english.md");
  if (rule === null) return [".omp/rules/english.md: not found — the allowed words live there"];
  const allowed = allowedWords(rule);
  const issues: string[] = [];
  for (const path of walkFiles()) {
    if (path.startsWith(".omp/state/")) continue;
    const source = kindOf(path) === null ? null : readText(path);
    if (source !== null) issues.push(...scanFile(path, source, allowed));
  }
  return issues;
}

const spec: PostCheck = {
  name: "check-english",
  check: checkEnglish,
  fixHint: "write documentation and code comments in English; list a proper noun under Allowed words in .omp/rules/english.md",
  passMessage: "documentation and code comments are English",
};

export default function checkEnglishHook(pi: ExtensionAPI): void {
  registerPostCheck(pi, spec);
}

runCheckCli(spec);
