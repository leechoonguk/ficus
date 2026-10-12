/**
 * Hook self-measurement — what the 36 post hooks actually cost and actually say.
 *
 * Until now every claim about hook behaviour was an estimate: "a few hundred ms
 * per edit", "34 checks passed" (7 of them dormant), "some hook may be dead".
 * `guarded()` already wraps every post hook's `tool_result` body, so it is the
 * one place that sees every run, its duration, and its outcome — measuring
 * there costs one `Date.now()` pair and needs no change in any hook.
 *
 * Records are aggregated in memory and flushed as one JSON line to
 * `.omp/state/hook-metrics.jsonl` (gitignored) at most every FLUSH_INTERVAL_MS,
 * so a session of 500 tool calls writes a bounded number of lines instead of
 * 18,000 records.
 *
 * Measured, not assumed: the runtime loads **each hook in its own module
 * instance**, so this module's state is per hook, not per session — one flushed
 * snapshot carries exactly one hook, and a 36-hook session start produces 36
 * writer ids within the same second. `make hook-report` therefore keeps the
 * newest snapshot per writer and sums across them.
 *
 * Outcomes are the three the wrapper can observe honestly: `quiet` (ran, found
 * nothing), `warned` (returned a notice), and `crashed`. Whether a run was
 * filtered out by the hook's own read-only tool guard is *not* inferred — each
 * hook owns a different filter — so the report separates `runs` from `worked`
 * (a run that took at least a millisecond, i.e. actually touched the tree). A
 * hook that never leaves `quiet` while its rule is being violated is the
 * failure this measurement exists to expose.
 *
 * Two counters answer the questions the outcome triple cannot. **warned→fixed**
 * keys an open finding by the file the tool call named, so a warning counts as
 * resolved only when the same hook goes quiet on a later write to *that* file —
 * without the file key, a warning on A followed by an edit to B reads as a fix.
 * **Rule access** counts what actually reaches the model through the rule
 * channel: `rule://<name>` and `.omp/rules/<name>.md` are the same content
 * behind two doors, and a session that edits the rule it read was authoring it,
 * not being guided by it. Measured before building this: 74 of 661 rule reads
 * across 87 sessions used `rule://`, and 443 of them were authoring — counting
 * either door alone, or reads without the authoring split, reports a channel
 * that is not the one being used.
 *
 * Both are recorded from `guarded()`, which sees every run; the rule counter is
 * keyed to one owning hook because 36 hook module instances would otherwise
 * count each read 36 times.
 */
// @ts-expect-error — Node builtin, not project-compiled
import { appendFileSync, existsSync, mkdirSync, readdirSync, readFileSync } from "node:fs";
// @ts-expect-error — Node builtin, not project-compiled
import { dirname, join } from "node:path";

const cwd = process.cwd();
const projectRoot = existsSync(join(cwd, ".omp")) ? cwd : join(cwd, "..");
const metricsLog = join(projectRoot, ".omp", "state", "hook-metrics.jsonl");

/** A flush per tool call would dominate the very latency being measured. */
const FLUSH_INTERVAL_MS = 15_000;

/** A run this slow is the reason an edit felt slow — worth counting on its own. */
const SLOW_RUN_MS = 250;

/** The rule channel's owning hook — recording from all 36 multiplies every read. */
const RULE_CHANNEL_OWNER = "check-omp-rules";

/** Tools whose arguments name the file a finding is about. */
const FILE_TOOLS = new Set(["edit", "write", "ast_edit"]);

/** Tools that can change the tree a whole-tree check reads. */
const TREE_TOOLS = new Set(["edit", "write", "ast_edit", "bash", "eval"]);

/** `rule://<name>` and `.omp/rules/<name>.md`, with an optional read selector. */
const RULE_PATH = /(?:^rule:\/\/|\.omp\/rules\/)([A-Za-z0-9._-]+?)(?:\.md)?(?::(\S*))?$/;

export type Outcome = "quiet" | "warned" | "crashed";

type Counters = {
  runs: number;
  worked: number;
  quiet: number;
  warned: number;
  crashed: number;
  slow: number;
  ms: number;
  maxMs: number;
  /** Closed warnings whose fix rewrote the warned file — strong attribution. */
  fixed: number;
  /** Closed warnings the hook stopped repeating without that file being touched. */
  cleared: number;
  /** Findings still open when the snapshot was written. */
  open: number;
};

type RuleReads = {
  reads: number;
  /** Reads that asked for a line range or `:raw` instead of the whole file. */
  ranged: number;
  /** Reads that went through `rule://` rather than the file path. */
  viaUri: number;
  /** 1 once this run edited the rule — its reads were authoring, not guidance. */
  authored: number;
};

type Event = { toolName?: string; input?: Record<string, unknown> } | undefined;

type Snapshot = {
  pid: string;
  started: string;
  at: string;
  hooks: Record<string, Counters>;
  rules?: Record<string, RuleReads>;
};

const counters = new Map<string, Counters>();
const openFindings = new Map<string, Set<string>>();
const ruleReads = new Map<string, RuleReads>();
const runId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
const started = new Date().toISOString();

let lastFlushAt = 0;
let dirty = false;

/**
 * Records one post-hook run. Never throws: measurement must not be able to take
 * down the hook it measures.
 */
export function record(hookName: string, outcome: Outcome, elapsedMs: number, event?: Event): void {
  try {
    const entry = counters.get(hookName) ?? {
      runs: 0, worked: 0, quiet: 0, warned: 0, crashed: 0, slow: 0, ms: 0, maxMs: 0, fixed: 0, cleared: 0, open: 0,
    };
    entry.runs += 1;
    entry[outcome] += 1;
    entry.ms += elapsedMs;
    entry.maxMs = Math.max(entry.maxMs, elapsedMs);
    if (elapsedMs >= 1) entry.worked += 1;
    if (elapsedMs >= SLOW_RUN_MS) entry.slow += 1;
    trackFinding(hookName, outcome, entry, event);
    counters.set(hookName, entry);
    if (hookName === RULE_CHANNEL_OWNER) trackRuleAccess(event);
    dirty = true;

    const now = Date.now();
    // A crash is the one outcome worth persisting immediately: the session may
    // end before the next interval, and that record is the evidence.
    if (outcome === "crashed" || now - lastFlushAt >= FLUSH_INTERVAL_MS) {
      lastFlushAt = now;
      flush();
    }
  } catch {
    // A metrics failure is not a hook failure.
  }
}

/**
 * Opens a finding on the file a warning followed, and closes every open finding
 * once the hook is quiet again after a call that could change the tree.
 *
 * Post hooks scan the whole tree, so a quiet run is proof the complaint is gone
 * — but not proof of *what* fixed it. Measured over 698 real warnings: only
 * ~10% were followed by a write to the warned file, 50% by a command (usually
 * the `make check-*` the warning asked for) and 38% by a write elsewhere. So a
 * close is `fixed` when this call named the warned file and `cleared` when it
 * did not; counting only the first would report a permanent zero.
 *
 * Read-only calls close nothing: a hook that says nothing after a `read` has
 * proven nothing about the tree.
 */
function trackFinding(hookName: string, outcome: Outcome, entry: Counters, event: Event): void {
  if (event?.toolName === undefined || !TREE_TOOLS.has(event.toolName)) return;
  const open = openFindings.get(hookName) ?? new Set<string>();
  const named = pathsOf(event.input);
  if (outcome === "warned") {
    for (const path of named) open.add(path);
  } else if (outcome === "quiet") {
    for (const path of open) {
      if (named.includes(path)) entry.fixed += 1;
      else entry.cleared += 1;
    }
    open.clear();
  }
  openFindings.set(hookName, open);
  entry.open = open.size;
}

/** Counts a rule read, or marks the rule as authored by this run. */
function trackRuleAccess(event: Event): void {
  const tool = event?.toolName;
  if (tool !== "read" && !FILE_TOOLS.has(tool ?? "")) return;
  for (const candidate of pathsOf(event?.input)) {
    const match = RULE_PATH.exec(candidate);
    if (match === null) continue;
    const entry = ruleReads.get(match[1]) ?? { reads: 0, ranged: 0, viaUri: 0, authored: 0 };
    if (tool === "read") {
      entry.reads += 1;
      if (match[2] !== undefined) entry.ranged += 1;
      if (candidate.startsWith("rule://")) entry.viaUri += 1;
    } else {
      entry.authored = 1;
    }
    ruleReads.set(match[1], entry);
  }
}

/**
 * The files a tool call names: `path` for read/write, and the `[path#TAG]`
 * section headers for a hashline edit, which carries no `path` of its own.
 */
function pathsOf(input: Record<string, unknown> | undefined): string[] {
  if (input === undefined) return [];
  const found: string[] = [];
  const path = input["path"];
  if (typeof path === "string" && path !== "") found.push(path);
  const hashline = input["input"];
  if (typeof hashline === "string") {
    for (const header of hashline.matchAll(/^\[([^\]\n]+?)#[0-9A-Fa-f]{4}\]/gm)) found.push(header[1]);
  }
  return found;
}

/** Writes the current snapshot as one line. Idempotent when nothing changed. */
export function flush(): void {
  if (!dirty) return;
  dirty = false;
  const hooks: Record<string, Counters> = {};
  for (const [hookName, entry] of counters) hooks[hookName] = entry;
  const rules: Record<string, RuleReads> = {};
  for (const [name, entry] of ruleReads) rules[name] = entry;
  const snapshot = { pid: runId, started, at: new Date().toISOString(), hooks, rules };
  try {
    mkdirSync(dirname(metricsLog), { recursive: true });
    appendFileSync(metricsLog, `${JSON.stringify(snapshot)}\n`);
  } catch {
    // Losing a metrics line must never surface as a hook warning.
  }
}

/** Newest snapshot per process — an earlier line is a strict subset of a later one. */
function readSnapshots(): Snapshot[] {
  if (!existsSync(metricsLog)) return [];
  const newest = new Map<string, Snapshot>();
  for (const line of readFileSync(metricsLog, "utf-8").split("\n")) {
    if (line.trim() === "") continue;
    try {
      const snapshot = JSON.parse(line) as Snapshot;
      newest.set(snapshot.pid, snapshot);
    } catch {
      continue;
    }
  }
  return [...newest.values()];
}

function totals(snapshots: Snapshot[]): Map<string, Counters> {
  const total = new Map<string, Counters>();
  for (const snapshot of snapshots) {
    for (const [hookName, entry] of Object.entries(snapshot.hooks)) {
      const sum = total.get(hookName);
      if (sum === undefined) {
        // Snapshots written before these counters existed carry no `fixed`,
        // `cleared` or `open`, and a spread would copy the hole into the sums.
        total.set(hookName, {
          ...entry,
          fixed: entry.fixed ?? 0,
          cleared: entry.cleared ?? 0,
          open: entry.open ?? 0,
        });
        continue;
      }
      sum.runs += entry.runs;
      sum.worked += entry.worked;
      sum.quiet += entry.quiet;
      sum.warned += entry.warned;
      sum.crashed += entry.crashed;
      sum.slow += entry.slow;
      sum.ms += entry.ms;
      sum.fixed += entry.fixed ?? 0;
      sum.cleared += entry.cleared ?? 0;
      sum.open += entry.open ?? 0;
      sum.maxMs = Math.max(sum.maxMs, entry.maxMs);
    }
  }
  return total;
}

/** Rule access summed across snapshots — authored anywhere means authored. */
function ruleTotals(snapshots: Snapshot[]): Map<string, RuleReads> {
  const total = new Map<string, RuleReads>();
  for (const snapshot of snapshots) {
    for (const [name, entry] of Object.entries(snapshot.rules ?? {})) {
      const sum = total.get(name);
      if (sum === undefined) {
        total.set(name, { ...entry });
        continue;
      }
      sum.reads += entry.reads;
      sum.ranged += entry.ranged;
      sum.viaUri += entry.viaUri;
      sum.authored = Math.max(sum.authored, entry.authored);
    }
  }
  return total;
}

/** Rule files on disk — the denominator for "never read for guidance". */
function countRuleFiles(): number {
  try {
    return readdirSync(join(projectRoot, ".omp", "rules")).filter((file: string) => file.endsWith(".md")).length;
  } catch {
    return 0;
  }
}

/**
 * What the rule channel actually delivered. Reads of a rule this run also
 * edited are authoring, not guidance, and both `rule://` and the file path are
 * the same door — reporting either half alone is how a used channel reads as a
 * dead one, and an authored one reads as a used one.
 */
function ruleChannel(snapshots: Snapshot[]): string[] {
  const entries = ruleTotals(snapshots);
  if (entries.size === 0) return [];
  let guidance = 0;
  let authoring = 0;
  let ranged = 0;
  let viaUri = 0;
  let guided = 0;
  for (const entry of entries.values()) {
    ranged += entry.ranged;
    viaUri += entry.viaUri;
    if (entry.authored === 1) {
      authoring += entry.reads;
      continue;
    }
    guidance += entry.reads;
    if (entry.reads > 0) guided += 1;
  }
  const lines = [
    `    rule channel: ${guidance} guidance read(s), ${authoring} authoring read(s), ` +
      `${ranged} paged, ${viaUri} via rule://`,
  ];
  const onDisk = countRuleFiles();
  if (onDisk > 0) {
    lines.push(`    ${onDisk - guided}/${onDisk} rule(s) never read for guidance in these session(s)`);
  }
  return lines;
}

function report(): string[] {
  const snapshots = readSnapshots();
  if (snapshots.length === 0) {
    return [
      "    no hook runs recorded yet — post hooks record through guarded() in the",
      "    session that loaded them, so this fills in after the next session's edits",
    ];
  }

  const rows = [...totals(snapshots).entries()].sort((left, right) => right[1].ms - left[1].ms);
  const lines = [
    `    ${snapshots.length} snapshot(s) from ${new Set(snapshots.map((s) => s.started.slice(0, 16))).size} session start(s), ${rows.length} hook(s) measured`,
    `    ${"hook".padEnd(42)}${"runs".padStart(6)}${"worked".padStart(8)}${"warned".padStart(8)}${"fixed".padStart(7)}${"clrd".padStart(6)}${"crash".padStart(7)}${"totalMs".padStart(9)}${"maxMs".padStart(8)}`,
  ];
  let worked = 0;
  let ms = 0;
  let warned = 0;
  let fixed = 0;
  let cleared = 0;
  let open = 0;
  for (const [hookName, entry] of rows) {
    worked += entry.worked;
    ms += entry.ms;
    warned += entry.warned;
    fixed += entry.fixed;
    cleared += entry.cleared;
    open += entry.open;
    lines.push(
      `    ${hookName.padEnd(42)}${String(entry.runs).padStart(6)}${String(entry.worked).padStart(8)}` +
        `${String(entry.warned).padStart(8)}${String(entry.fixed).padStart(7)}${String(entry.cleared).padStart(6)}` +
        `${String(entry.crashed).padStart(7)}${entry.ms.toFixed(0).padStart(9)}${entry.maxMs.toFixed(0).padStart(8)}`,
    );
  }
  lines.push(`    ${"TOTAL".padEnd(42)}${"".padStart(6)}${String(worked).padStart(8)}${"".padStart(15)}${ms.toFixed(0).padStart(9)}`);
  if (worked > 0) {
    lines.push(`    ${(ms / worked).toFixed(1)}ms per working run — the latency an edit pays per hook that looks at the tree`);
  }

  if (warned > 0) {
    lines.push(
      `    warned→closed: ${fixed} fixed (warned file rewritten), ${cleared} cleared elsewhere, ` +
        `${open} still open of ${warned} warning(s)`,
    );
  }
  const silent = rows.filter(([, entry]) => entry.warned === 0 && entry.worked > 0);
  if (silent.length > 0) {
    lines.push(
      `    ${silent.length}/${rows.length} hook(s) ran and found nothing — expected on a clean tree, and evidence ` +
        `of nothing on its own: ${silent.map(([name]) => name).join(", ")}`,
    );
  }
  lines.push(...ruleChannel(snapshots));
  const slow = rows.filter(([, entry]) => entry.slow > 0);
  if (slow.length > 0) {
    lines.push(`    slow runs (>=${SLOW_RUN_MS}ms): ${slow.map(([name, entry]) => `${name} x${entry.slow}`).join(", ")}`);
  }
  const crashed = rows.filter(([, entry]) => entry.crashed > 0);
  if (crashed.length > 0) {
    lines.push(`    CRASHED: ${crashed.map(([name, entry]) => `${name} x${entry.crashed}`).join(", ")} — see .omp/state/hook-crash.log`);
  }
  return lines;
}

// Direct execution (make hook-report): print what the hooks actually did.
if (typeof process !== "undefined" && process.argv[1]?.endsWith("hook-metrics.ts")) {
  console.log("==> hook-report");
  for (const line of report()) console.log(line);
  process.exit(0);
}
