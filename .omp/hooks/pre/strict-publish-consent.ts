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

// 앵커에 인용부호·백틱 포함 — eval 본문(execSync("git push"))에서는 셸 구분자만으로 시작점을 못 잡는다.
const RELEASE = /(?:^|[&|;()"'`]\s*)make\s+(?:-\S+\s+)*(?:release|unrelease)\b/;

const PUBLISH =
  /(?:^|[&|;()"'`]\s*)(?:sudo\s+)?(?:git(?:\s+-\S+(?:\s+\S+)?)*\s+(?:commit|push)|make\s+(?:-\S+\s+)*(?:commit|push)\b)/;

export default function strictPublishConsent(pi: ExtensionAPI): void {
  type PublishEvent = { toolName?: string; input?: Record<string, unknown> };
  type PublishContext = { sessionManager?: { getBranch?: () => BranchEntry[] } };

  // ctx-ok: 발행 동의 판정은 세션 브랜치(sessionManager.getBranch)를 읽어야 한다 — event 에는 없다.
  // 모든 접근이 옵셔널 체이닝이라 던질 수 없다(아래 ?? null).
  pi.on("tool_call", (event: PublishEvent, ctx: PublishContext) => {
    // 실행 채널은 bash 만이 아니다 — eval 본문(subprocess.run/execSync)로도 push 가 나간다
    // (sandbox 전수 점검: 라우팅 우회의 32% 가 eval). 같은 판정을 두 채널에 건다.
    if (event.toolName !== "bash" && event.toolName !== "eval") return;
    const body = String(event.input?.command ?? event.input?.code ?? "");
    // argv 리스트형(subprocess.run(["git","push"…]))은 연속 문자열이 아니다 — 실행 함수 + 대상
    // 토큰 공존으로 판정한다(strict-verified-git 과 같은 계약).
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
