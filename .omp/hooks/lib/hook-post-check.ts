/**
 * Post-check scaffold — the two entry points every `check-*` hook shares.
 *
 * A post hook is one pure function `check(): string[]` run two ways:
 *   - as a `tool_result` handler, inside `guarded()` so a crash is reported
 *     instead of silently disabling the check (hook-visibility.ts), with the
 *     warning shaped by the safety guard so it cannot drown the tool output;
 *   - as a CLI (`node .omp/hooks/post/check-<name>.ts`, run by `make check-<name>`
 *     and by `make verify`), exiting 1 on any issue.
 *
 * fast-api-standard repeats this body in every hook; here it lives once, so a
 * check cannot drift from the contract (`check-omp-hooks` requires both calls).
 */
import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { buildSafeWarning, resetSuppression, wrapWarning } from "./hook-safety-guard.ts";
import { guarded } from "./hook-visibility.ts";
import { printRuleHint } from "./hook-rule-hint.ts";
import { MUTATING_TOOLS } from "./hook-mutating-tools.ts";
import { touchesStrict } from "./hook-scope.ts";

type ContentChunk = { type: string; text?: string };
type ToolResultEvent = { toolName?: string; input?: Record<string, unknown>; content?: ContentChunk[] };

/** What a post check declares about itself. */
export interface PostCheck {
  /** `check-<name>` — also the make target and the file name. */
  name: string;
  /** Repo-relative issue lines; empty means the tree satisfies the check. */
  check: () => string[];
  /** One line telling the agent how to fix what was reported. */
  fixHint: string;
  /** Printed by the CLI when nothing is wrong. */
  passMessage: string;
  /** Path fragments the check reads; omitted = any mutating call can matter. */
  scope?: string[];
}

/** Registers the `tool_result` handler for a post check. */
export function registerPostCheck(pi: ExtensionAPI, spec: PostCheck): void {
  pi.on("tool_result", (event: ToolResultEvent) => guarded(spec.name, event, () => {
    // Read-only tools never change files — checking after them only spends context.
    if (event.toolName && !MUTATING_TOOLS.has(event.toolName)) return undefined;
    if (spec.scope && !touchesStrict(event, spec.scope)) return undefined;
    const issues = spec.check();
    if (issues.length === 0) {
      resetSuppression(spec.name);
      return undefined;
    }
    const warning = buildSafeWarning(event.toolName, spec.name, issues, spec.fixHint);
    if (warning === null) return undefined;
    return { content: wrapWarning(warning, event.content, spec.name) };
  }));
}

/** Runs the check as a CLI when this process was started on the hook's own file. */
export function runCheckCli(spec: PostCheck): void {
  if (!process.argv[1]?.endsWith(`${spec.name}.ts`)) return;
  const issues = spec.check();
  console.log(`==> ${spec.name}`);
  if (issues.length === 0) {
    console.log(`    PASS: ${spec.passMessage}`);
    return;
  }
  for (const issue of issues) console.log(`    FAIL: ${issue}`);
  printRuleHint(spec.name);
  process.exit(1);
}
