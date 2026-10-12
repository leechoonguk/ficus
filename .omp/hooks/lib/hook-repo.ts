/**
 * Repository facts the post checks read — the file tree and the Makefile. Parsed
 * in one place so two checks cannot disagree about what a target is.
 */
// @ts-expect-error — Node builtin, not project-compiled
import { existsSync, readdirSync, readFileSync } from "node:fs";
// @ts-expect-error — Node builtin, not project-compiled
import { join } from "node:path";
import { resolveProjectRoot, WALK_EXCLUDES } from "./hook-project-root.ts";

export const projectRoot = resolveProjectRoot();

/** File contents, or null when absent — a check reports the absence itself. */
export function readText(relPath: string): string | null {
  const path = join(projectRoot, relPath);
  if (!existsSync(path)) return null;
  return readFileSync(path, "utf-8");
}

/** Every file under `relDir` (default: the root), as repo-relative paths. */
export function walkFiles(relDir = ""): string[] {
  const found: string[] = [];
  const base = join(projectRoot, relDir);
  if (!existsSync(base)) return found;
  for (const entry of readdirSync(base, { withFileTypes: true })) {
    if (WALK_EXCLUDES[entry.name] === true) continue;
    const rel = relDir === "" ? entry.name : `${relDir}/${entry.name}`;
    if (entry.isDirectory()) found.push(...walkFiles(rel));
    else found.push(rel);
  }
  return found;
}

/** One Makefile rule as written. */
export interface MakeTarget {
  name: string;
  /** Text after `##`, or null when the target has no help line. */
  help: string | null;
  prerequisites: string[];
  /** Recipe lines without the leading tab. */
  recipe: string[];
}

/**
 * Explicit rules of a Makefile, keyed by target. Variable assignments
 * (`x ?= 1`, `x := 1`), target-specific variables (`t: x = 1`) and special
 * targets (`.PHONY`) are not rules.
 */
export function parseMakefile(text: string): Map<string, MakeTarget> {
  const targets = new Map<string, MakeTarget>();
  let current: MakeTarget | null = null;
  for (const line of text.replace(/\\\r?\n/g, " ").split(/\r?\n/)) {
    if (line.startsWith("\t")) {
      current?.recipe.push(line.slice(1));
      continue;
    }
    const match = /^([A-Za-z][\w-]*)\s*:(?![:=])\s*([^#=]*?)\s*(?:##\s*(.*))?$/.exec(line);
    if (match === null) {
      if (line.trim() !== "" && !line.startsWith("#")) current = null;
      continue;
    }
    const existing = targets.get(match[1]);
    current = existing ?? { name: match[1], help: null, prerequisites: [], recipe: [] };
    if (match[3] !== undefined) current.help = match[3].trim();
    current.prerequisites.push(...match[2].split(/\s+/).filter(Boolean));
    targets.set(match[1], current);
  }
  return targets;
}

/** Names listed in `.PHONY:` lines, backslash continuations joined. */
export function phonyTargets(text: string): string[] {
  return [...text.replace(/\\\r?\n/g, " ").matchAll(/^\.PHONY:\s*(.+)$/gm)]
    .flatMap((m) => m[1].split(/\s+/).filter(Boolean));
}
