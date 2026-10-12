import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
// @ts-expect-error — Node builtin, not project-compiled
import { spawnSync } from "node:child_process";

// 앵커에 인용부호(`"'`)와 백틱을 포함한다 — eval 본문에서는 명령이 문자열 리터럴 안에 있어
// (execSync("git push …")) 셸 구분자만으로는 시작점을 못 잡는다(실측: eval 채널 무차단).
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
  // ctx-ok: 커밋 검증은 작업 트리 경로(ctx.cwd)가 필요하다 — event 에 없고, 단순 문자열 읽기라 던질 수 없다.
  pi.on("tool_call", (event: { toolName?: string; input?: Record<string, unknown> }, ctx: { cwd: string }) => {
    // channel: bash + eval — eval 본문의 execSync("git push") 는 bash 필터를 그대로 지나친다.
    if (event.toolName !== "bash" && event.toolName !== "eval") return;

    const command = String(event.input?.command ?? event.input?.code ?? "");
    // eval 의 argv 리스트형(subprocess.run(["git","commit",…]))은 "git commit" 이 연속 문자열로
    // 존재하지 않는다 — strict-command-routing 과 같은 판정: 실행 함수와 대상 토큰이 같은 본문에
    // 함께 있으면 발행 시도로 본다.
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
    // ficus 는 단일 프로젝트라 게이트의 루트가 곧 작업 디렉터리다.

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
