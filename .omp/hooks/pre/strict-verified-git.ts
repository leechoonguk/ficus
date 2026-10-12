import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
// @ts-expect-error — Node builtin, not project-compiled
import { spawnSync } from "node:child_process";

// The anchor includes quotes (`"'`) and backticks: in an eval body the command sits
// inside a string literal (execSync("git push …")), where shell separators alone do
// not mark its start (measured: the eval channel went unblocked).
const GIT_PUBLISH = /(?:^|[&|;()"'`]\s*)(?:sudo\s+)?git(?:\s+-\S+(?:\s+\S+)?)*\s+(?:commit|push)\b/;
const GIT_COMMIT = /(?:^|[&|;()]\s*)(?:sudo\s+)?git(?:\s+-\S+(?:\s+\S+)?)*\s+commit\b/;

/**
 * Blocks OMP-mediated Git commits and pushes unless the current workspace passes
 * the root quality gate immediately before the Git command executes.
 *
 * `make verify` reads the **worktree**, so a commit is only covered by it when
 * the commit *is* the worktree: an unstaged change or an untracked file means
 * the verified tree and the committed tree are different trees, and the gate
 * proved nothing about the one being published. This session's own
 * `lib/hook-visibility.ts` was that case — untracked while 36 tracked hooks
 * imported it, so committing only the hooks would have published a tree that
 * cannot even type-check, with a green verify behind it.
 */
export default function strictVerifiedGit(pi: ExtensionAPI): void {
  // ctx-ok: verifying a commit needs the worktree path (ctx.cwd) — not on the event; a plain string read cannot throw.
  pi.on("tool_call", (event: { toolName?: string; input?: Record<string, unknown> }, ctx: { cwd: string }) => {
    // channel: bash + eval — execSync("git push") in an eval body passes a bash-only filter.
    if (event.toolName !== "bash" && event.toolName !== "eval") return;

    const command = String(event.input?.command ?? event.input?.code ?? "");
    // An argv list in eval (subprocess.run(["git","commit",…])) never contains "git commit"
    // as one string — so, as in strict-command-routing, an exec function together with the
    // target tokens in the same body counts as a publish attempt.
    const evalPublish = event.toolName === "eval"
      && /\b(execSync|exec|spawnSync|spawn|subprocess\.(?:run|call|check_call|Popen)|os\.system|popen)\b/.test(command)
      && /["'`]git["'`]/.test(command)
      && /["'`](commit|push)\b/.test(command);
    if (!GIT_PUBLISH.test(command) && !evalPublish) return;

    // Cheap first: a mismatched index makes the 25s verify irrelevant anyway.
    if (GIT_COMMIT.test(command)) {
      const mismatch = describeUncommittedWork(ctx.cwd);
      if (mismatch !== null) return { block: true, reason: mismatch };
    }
    // ficus is a single project: the gate's root is the working directory.

    const result = spawnSync("make", ["verify"], {
      cwd: ctx.cwd,
      encoding: "utf-8",
      timeout: 10 * 60 * 1000,
    });
    if (result.status === 0) return;

    return {
      block: true,
      reason:
        `make verify failed (exit ${result.status ?? "unknown"}); ` +
        "Git commit/push is blocked until verification passes.",
    };
  });
}

/**
 * The reason string when the commit would not be the verified tree, or null.
 *
 * Partial staging is a legitimate workflow (the commit skill says to stage only
 * the approved scope), so the block names both ways out — stage the rest, or
 * move it out of the way — instead of forcing one of them.
 */
function describeUncommittedWork(cwd: string): string | null {
  const status = spawnSync("git", ["status", "--porcelain"], { cwd, encoding: "utf-8" });
  if (status.status !== 0) return null;

  const unstaged: string[] = [];
  const untracked: string[] = [];
  for (const line of String(status.stdout ?? "").split("\n")) {
    if (line.trim() === "") continue;
    const path = line.slice(3);
    if (line.startsWith("??")) untracked.push(path);
    else if (line[1] !== " ") unstaged.push(path);
  }
  if (unstaged.length === 0 && untracked.length === 0) return null;

  const sample = [...unstaged, ...untracked].slice(0, 5).join(", ");
  const more = unstaged.length + untracked.length - 5;
  return (
    `the commit would not be the verified tree: ${unstaged.length} unstaged change(s) and ` +
    `${untracked.length} untracked file(s) (${sample}${more > 0 ? `, +${more} more` : ""}). ` +
    "`make verify` reads the worktree, so an index that differs from it was never verified — " +
    "run `git add -A` to commit what was verified, or `git stash -k` to move the rest out first."
  );
}
