/**
 * Hook scope — a hook fires only for paths in its own domain.
 *
 * Measured (per-prompt probe, in the harness this was ported from): 36 post
 * hooks attached to one mutating tool result cost 6.8 s per write and 4.9 s per
 * bash call, 22-36% of session wall time. Half of it was harness meta checks
 * (.omp/ structure, types) unrelated to the event — they ran after a one-line
 * backend edit.
 *
 * `touchesPath` filters only events that carry a path (write/edit) and lets
 * shell calls through (so files a command creates are not missed).
 * `touchesStrict` also reads the shell command string and skips when its
 * domain does not appear in it — only for checks with narrow, certain targets
 * such as the harness meta checks.
 */

type HookEvent = { toolName?: string; input?: Record<string, unknown> };

const SHELL_TOOLS = new Set(["bash", "eval"]);

function pathOf(event: HookEvent): string {
  const i = event.input ?? {};
  return String(i.path ?? i.file_path ?? i.filePath ?? i.target ?? "");
}

function commandOf(event: HookEvent): string {
  const i = event.input ?? {};
  return String(i.command ?? i.code ?? "");
}

/** Filters only events with a known path; shell and pathless events always pass (conservative). */
export function touchesPath(event: HookEvent, prefixes: string[]): boolean {
  const p = pathOf(event);
  if (!p) return true;
  return prefixes.some((pre) => p.includes(pre));
}

/** Also reads shell command strings; skips when the domain appears nowhere. */
export function touchesStrict(event: HookEvent, prefixes: string[]): boolean {
  const p = pathOf(event);
  if (p) return prefixes.some((pre) => p.includes(pre));
  if (SHELL_TOOLS.has(String(event.toolName ?? ""))) {
    const c = commandOf(event);
    if (!c) return true;
    return prefixes.some((pre) => c.includes(pre));
  }
  return true;
}
