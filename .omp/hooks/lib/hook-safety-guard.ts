/**
 * Hook safety guard — shared by all post hooks to prevent infinite loops.
 *
 * Problem: when a post hook injects a warning into tool_result content, the
 * warning can visually overshadow the original tool output. The agent sees
 * only the warning, not the result, and repeats the same action — infinite
 * loop.
 *
 * Guards:
 *   1. Warning summary: at most 3 issue lines (rest truncated with a count).
 *   2. Content protection: if the original tool output is longer than 200
 *      chars, the warning is shortened to a 1-line summary + "run make check-X".
 *   3. Per-hook suppression: after the same warning is injected 2 times,
 *      suppress it across every tool until the issues change or resolve.
 */

import { ruleForHook } from "./hook-rule-hint.ts";

const MAX_ISSUE_LINES = 3;
const CONTENT_PROTECT_THRESHOLD = 200;
const MAX_REPEAT_PER_HOOK = 2;

const suppressMap = new Map<string, number>();
/**
 * Builds a safe warning string from issues, truncating to MAX_ISSUE_LINES.
 * Returns null when the warning should be suppressed (repeat limit hit).
 *
 * Canonical shape: `buildSafeWarning(event.toolName, hookName, issues, fixHint)`
 * — enforced statically by `check-omp-hooks`.
 *
 * The arguments are normalized rather than trusted. A hook that drops
 * `toolName` shifts the issue array onto `hookName` and a string onto `issues`;
 * indexing a string then threw `visible.map is not a function` from inside this
 * file, which killed the whole hook — every check it owns silently stopped
 * running, and the message named no hook. Degrading is the correct failure mode
 * for a guard: the warning still reaches the agent, and the static check reports
 * the wrong call shape by file name.
 */
export function buildSafeWarning(
  toolNameOrHookName: string | undefined,
  hookNameOrIssues: string | string[],
  issuesOrFixHint: string[] | string,
  fixHint?: string,
): string | null {
  const shifted = Array.isArray(hookNameOrIssues);
  const hookName = shifted ? String(toolNameOrHookName ?? "hook") : String(hookNameOrIssues);
  const issues = shifted
    ? hookNameOrIssues
    : Array.isArray(issuesOrFixHint)
      ? issuesOrFixHint
      : [String(issuesOrFixHint)];
  const hint = shifted ? String(issuesOrFixHint ?? "") : String(fixHint ?? `make ${hookName}`);
  if (shifted) {
    console.error(
      `[hook-safety-guard] ${hookName}: buildSafeWarning() called without event.toolName — ` +
        "shape is (event.toolName, hookName, issues, fixHint); run make check-omp-hooks",
    );
  }
  if (issues.length === 0) return null;

  const visible = issues.slice(0, MAX_ISSUE_LINES);
  const truncated = issues.length > MAX_ISSUE_LINES
    ? `  ... (${issues.length - MAX_ISSUE_LINES} more — run make ${hookName})`
    : "";
  const rule = ruleForHook(hookName);
  const summary = [
    `[${hookName}] ${issues.length} issue(s):`,
    ...visible.map((issue) => `  - ${issue}`),
    truncated,
    ...(rule ? [`Rule: rule://${rule}`] : []),
    `Fix: ${hint}`,
  ].join("\n");

  // Per-hook suppression prevents tool hopping from bypassing the loop guard.
  // Key by hook and complete issue summary so tool changes cannot restart a loop.
  const key = `${hookName}\u0000${summary}`;
  const count = (suppressMap.get(key) ?? 0) + 1;
  suppressMap.set(key, count);
  if (count > MAX_REPEAT_PER_HOOK) {
    return null;
  }
  return summary;
}

/**
 * Wraps the warning in a system-notice, respecting content protection.
 * If the original content is long (>200 chars), the warning is collapsed
 * to a single line so it doesn't overshadow the tool output.
 */
export function wrapWarning(
  warning: string,
  originalContent: Array<{ type: string; text?: string }> | undefined,
  hookName: string,
): Array<{ type: string; text?: string }> {
  const originalLen = (originalContent ?? [])
    .map((c) => c.text ?? "")
    .join("").length;

  const text = originalLen > CONTENT_PROTECT_THRESHOLD
    ? `\n<system-notice>\n[${hookName}] ${warning.split("\n")[0]} — run "make ${hookName}" for details\n</system-notice>`
    : `\n<system-notice>\n${warning}\n</system-notice>`;

  return [
    ...(originalContent ?? []),
    { type: "text", text },
  ];
}

/**
 * Resets suppression state — call when issues resolve (empty issues).
 */
export function resetSuppression(hookName: string): void {
  for (const key of suppressMap.keys()) {
    if (key.startsWith(`${hookName}\u0000`)) {
      suppressMap.delete(key);
    }
  }
}
