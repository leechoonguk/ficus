/**
 * Hook crash visibility — the wrapper every post hook's handler body runs in.
 *
 * The extension runner catches a `tool_result` handler's exception, hands it to
 * `emitError(...)` and returns: the throw never reaches the conversation and the
 * handler's `{ content }` override is discarded. Measured in the runtime bundle
 * (`dist/cli.js`), both the throw path and the handler-timeout path end that
 * way. So a post hook that dies on its first tool call keeps printing nothing,
 * keeps its Makefile target green when run by hand, and enforces nothing at all
 * for the rest of the session — a dead check reads exactly like a passing one.
 * That already happened here once: a `buildSafeWarning()` call missing its
 * `toolName` argument threw `visible.map is not a function` from inside the
 * safety guard, and the hook stayed silent.
 *
 * `guarded` closes the gap. A throw becomes:
 *   1. a one-line `<system-notice>` appended to the tool result, so the model
 *      sees that the check is down instead of trusting its silence,
 *   2. a line in `.omp/state/hook-crash.log`, so it outlives the session,
 *   3. a stderr line, so a CLI run and the runtime log both carry it.
 *
 * The crash is reported, never rethrown: one broken hook must not decide what
 * the other 34 get to say. `check-omp-hooks` requires every post hook to route
 * its handler body through here.
 */
// @ts-expect-error — Node builtin, not project-compiled
import { appendFileSync, existsSync, mkdirSync } from "node:fs";
// @ts-expect-error — Node builtin, not project-compiled
import { dirname, join } from "node:path";
import { record, type Outcome } from "./hook-metrics.ts";

const cwd = process.cwd();
const projectRoot = existsSync(join(cwd, ".omp")) ? cwd : join(cwd, "..");
const crashLog = join(projectRoot, ".omp", "state", "hook-crash.log");

/**
 * A crash repeats on every following tool call, so the notice is capped while
 * the log keeps every occurrence: two turns is enough for the agent to act, and
 * an uncapped notice would push the tool output it is attached to out of view.
 */
const MAX_NOTICES_PER_CRASH = 2;

/** Notice budget per hook + error message, so a *different* crash still speaks. */
const noticeCounts = new Map<string, number>();

type ContentChunk = { type: string; text?: string };
type GuardedEvent = { toolName?: string; input?: Record<string, unknown>; content?: ContentChunk[] } | undefined;
type ContentOverride = { content: ContentChunk[] };

/**
 * Runs a post hook's `tool_result` body and reports a throw instead of losing
 * it. The body's own return value passes through untouched, so the guard is
 * invisible until something breaks.
 *
 * Every run is also measured here — this is the only place that sees all of
 * them — so `make hook-report` can say what the hooks cost, which warnings were
 * acted on, and what the rule channel delivered. The event is handed to the
 * recorder because its `input` names the file a finding is about; the content
 * override the body may return says nothing about which file it was.
 *
 * A body that returns a promise is awaited through the same path: every handler
 * in this repository is synchronous today, and a future async one must not
 * silently reopen the hole this wrapper exists to close.
 */
export function guarded<T>(hookName: string, event: GuardedEvent, body: () => T): T | ContentOverride {
  const startedAt = Date.now();
  try {
    const result = body();
    if (isThenable(result)) {
      // The rejection is reported as the same override the sync path returns;
      // `as T` is the promise-of-union the caller's own signature already is.
      return (result as PromiseLike<unknown>).then(
        (value) => {
          record(hookName, outcomeOf(value), Date.now() - startedAt, event);
          return value;
        },
        (error: unknown) => {
          record(hookName, "crashed", Date.now() - startedAt, event);
          return report(hookName, event, error);
        },
      ) as T;
    }
    record(hookName, outcomeOf(result), Date.now() - startedAt, event);
    return result;
  } catch (error) {
    record(hookName, "crashed", Date.now() - startedAt, event);
    return report(hookName, event, error);
  }
}

/** A returned `content` override is the hook speaking; anything else is silence. */
function outcomeOf(result: unknown): Outcome {
  const spoke = typeof result === "object" && result !== null && "content" in result;
  return spoke ? "warned" : "quiet";
}

function report(hookName: string, event: GuardedEvent, error: unknown): ContentOverride {
  const message = error instanceof Error ? error.message : String(error);
  const stack = error instanceof Error ? (error.stack ?? "") : "";
  const origin = firstOwnFrame(stack);
  // The event is whatever the runner handed the handler. Reading it must not be
  // the second throw — that one would escape the guard and be invisible again —
  // and a field that cannot be read must not take the other one down with it.
  const tool = read(() => event?.toolName ?? "unknown", "unknown");
  const original = read(() => event?.content ?? [], [] as ContentChunk[]);
  const summary = `[${hookName}] HOOK CRASHED — its checks are NOT running: ${message}${origin}`;

  appendCrashLog(`${new Date().toISOString()} ${summary} (after ${tool})\n${stack}\n`);
  console.error(summary);

  const key = `${hookName}\u0000${message}`;
  const count = (noticeCounts.get(key) ?? 0) + 1;
  noticeCounts.set(key, count);
  if (count > MAX_NOTICES_PER_CRASH) return { content: original };

  const notice =
    `\n<system-notice>\n${summary} — fix the hook (details in .omp/state/hook-crash.log), ` +
    `then re-run "make ${hookName}"\n</system-notice>`;
  return { content: [...original, { type: "text", text: notice }] };
}

/** A value the guard needs but does not control: absent and unreadable are the same. */
function read<T>(get: () => T, fallback: T): T {
  try {
    return get();
  } catch {
    return fallback;
  }
}

/**
 * The frame that names the hook's own file. A bare message ("undefined is not
 * iterable") sends a reader to grep 11k lines of hooks; the frame is the fix
 * location.
 */
function firstOwnFrame(stack: string): string {
  const frame = stack.split("\n").find((line) => /\.omp\/hooks\//.test(line));
  if (frame === undefined) return "";
  const location = /\.omp\/(hooks\/[^\s)]+)/.exec(frame);
  return location === null ? "" : ` (at ${location[1]})`;
}

/**
 * Logging must never be the thing that kills the hook it is reporting on, so a
 * write failure degrades to stderr only.
 */
function appendCrashLog(line: string): void {
  try {
    mkdirSync(dirname(crashLog), { recursive: true });
    appendFileSync(crashLog, line);
  } catch (error) {
    console.error(`[hook-visibility] cannot write ${crashLog}: ${String(error)}`);
  }
}

function isThenable(value: unknown): boolean {
  return typeof value === "object" && value !== null && typeof (value as { then?: unknown }).then === "function";
}
