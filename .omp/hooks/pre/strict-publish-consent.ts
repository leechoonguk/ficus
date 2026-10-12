import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import type { BranchEntry } from "../lib/hook-publish-consent.ts";
import { publishConsent, releaseConsent } from "../lib/hook-publish-consent.ts";

/**
 * Publishing without being asked, blocked at the tool boundary.
 *
 * `strict-verified-git.ts` answers "is this tree green"; this answers "did
 * anyone ask for it". They are different failures: this session published five
 * commits nobody requested, every one of them green.
 *
 * Both routes are covered because both are bash commands: the raw `git commit` /
 * `git push`, and the `make commit` / `make push` targets that own the verify
 * prerequisite. A publish that never reaches the bash tool - a user running make
 * in their own terminal - is the user's own action and needs no gate.
 *
 * A release (`make release`, and its rollback `make unrelease`) needs release
 * words in the current turn: a request to commit is not a request to publish
 * binaries to every user. Raw tag and GitHub-release commands never get this far
 * as an agent action — `strict-release-route.ts` sends them through make.
 */

// The anchor includes quotes and backticks: in an eval body (execSync("git push")) shell separators alone do not mark the start.
const RELEASE = /(?:^|[&|;()"'`]\s*)make\s+(?:-\S+\s+)*(?:release|unrelease)\b/;

const PUBLISH =
  /(?:^|[&|;()"'`]\s*)(?:sudo\s+)?(?:git(?:\s+-\S+(?:\s+\S+)?)*\s+(?:commit|push)|make\s+(?:-\S+\s+)*(?:commit|push)\b)/;

export default function strictPublishConsent(pi: ExtensionAPI): void {
  type PublishEvent = { toolName?: string; input?: Record<string, unknown> };
  type PublishContext = { sessionManager?: { getBranch?: () => BranchEntry[] } };

  // ctx-ok: deciding consent needs the session branch (sessionManager.getBranch) — not on the event.
  // Every access is optional chaining, so it cannot throw (?? null below).
  pi.on("tool_call", (event: PublishEvent, ctx: PublishContext) => {
    // bash is not the only channel — a push also leaves through an eval body (subprocess.run/
    // execSync; a sandbox audit found 32% of routing bypasses in eval). Same decision on both.
    if (event.toolName !== "bash" && event.toolName !== "eval") return;
    const body = String(event.input?.command ?? event.input?.code ?? "");
    // An argv list (subprocess.run(["git","push"…])) is not one string — decide on an exec
    // function together with the target tokens (the same contract as strict-verified-git).
    const evalPublish = event.toolName === "eval"
      && /\b(execSync|exec|spawnSync|spawn|subprocess\.(?:run|call|check_call|Popen)|os\.system|popen)\b/.test(body)
      && /["'`]git["'`]/.test(body)
      && /["'`](commit|push)\b/.test(body);
    // A dry run (`make release … DRY_RUN=1`) checks and prints, it publishes nothing.
    const release = RELEASE.test(body) && !(/\bmake\s[^\n;&|]*\brelease\b[^\n;&|]*\bDRY_RUN=1\b/.test(body) && !/\bunrelease\b/.test(body));
    if (!PUBLISH.test(body) && !evalPublish && !release) return;

    const entries = ctx.sessionManager?.getBranch?.() ?? null;
    if (entries === null) {
      return {
        block: true,
        reason:
          "publish blocked: the session history is unreadable, so consent cannot be established — " +
          "ask the user to confirm, or let them run `make commit` / `make push` themselves",
      };
    }

    if (release) {
      const verdict = releaseConsent(entries);
      if (verdict.granted) return;
      return {
        block: true,
        reason:
          `release blocked: ${verdict.reason}. Report what would be released and ask; ` +
          "when the user asks for a release, `make release bump=patch|minor|major` runs the gate and the checks first.",
      };
    }

    const verdict = publishConsent(entries);
    if (verdict.granted) return;

    return {
      block: true,
      reason:
        `publish blocked: ${verdict.reason}. Report the work and ask; ` +
        "when the user asks, `make commit m=\"...\"` and `make push` run the gate first.",
    };
  });
}
