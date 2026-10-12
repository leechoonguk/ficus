import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { registerPostCheck, runCheckCli, type PostCheck } from "../lib/hook-post-check.ts";
import { ruleForHook } from "../lib/hook-rule-hint.ts";
import { readText, walkFiles } from "../lib/hook-repo.ts";

/**
 * Check-Omp-Rules Post Hook
 *
 * Rules reach the model through exactly three channels (AGENTS.md), and a rule
 * that fits none of them is never delivered (rule://omp-conventions):
 *
 *   1. Every `.omp/rules/<name>.md` opens with frontmatter carrying a
 *      `description` and one channel: `globs` (rulebook), `alwaysApply: true`,
 *      or `condition` (TTSR trigger).
 *   2. At most two always-apply rules — each costs its full body in every session.
 *   3. Every post check names a rule that exists (`check-<x>` → `<x>.md`, or the
 *      mapping in hook-rule-hint.ts), so a failure points at the invariant it
 *      enforces instead of making the agent rediscover it.
 *   4. A rule that cites `rule://<name>` cites a rule that exists.
 */

const MAX_ALWAYS_APPLY = 2;

export function checkOmpRules(): string[] {
  const issues: string[] = [];
  const rules = walkFiles(".omp/rules").filter((path) => path.endsWith(".md"));
  const names = new Set(rules.map((path) => /([\w-]+)\.md$/.exec(path)?.[1] ?? ""));
  let alwaysApply = 0;

  for (const path of rules) {
    const source = readText(path) ?? "";
    const frontmatter = /^---\n([\s\S]*?)\n---\n/.exec(source)?.[1];
    if (frontmatter === undefined) {
      issues.push(`${path}: no frontmatter — a rule without one is never delivered`);
      continue;
    }
    if (!/^description:\s*\S/m.test(frontmatter)) issues.push(`${path}: frontmatter has no description`);
    const always = /^alwaysApply:\s*true\s*$/m.test(frontmatter);
    if (always) alwaysApply += 1;
    if (!always && !/^globs:/m.test(frontmatter) && !/^condition:/m.test(frontmatter)) {
      issues.push(`${path}: no channel — add globs (rulebook), alwaysApply: true, or condition (TTSR)`);
    }
  }
  if (alwaysApply > MAX_ALWAYS_APPLY) {
    issues.push(`.omp/rules: ${alwaysApply} always-apply rules (max ${MAX_ALWAYS_APPLY}) — each is a full body in every session`);
  }

  for (const path of walkFiles(".omp/hooks/post")) {
    const hook = /([\w-]+)\.ts$/.exec(path)?.[1];
    if (hook !== undefined && ruleForHook(hook) === null) {
      issues.push(`${path}: enforces no rule in .omp/rules/ — document the invariant it checks`);
    }
  }

  for (const path of walkFiles(".omp").filter((file) => file.endsWith(".md"))) {
    for (const m of (readText(path) ?? "").matchAll(/rule:\/\/([\w-]+)/g)) {
      if (m[1] !== "name" && !names.has(m[1])) issues.push(`${path}: cites rule://${m[1]}, which does not exist`);
    }
  }
  return issues;
}

const spec: PostCheck = {
  name: "check-omp-rules",
  check: checkOmpRules,
  fixHint: "give every rule frontmatter with a description and a channel; give every check a rule",
  passMessage: "rule frontmatter, channels and check→rule mapping hold",
  scope: [".omp/"],
};

export default function checkOmpRulesHook(pi: ExtensionAPI): void {
  registerPostCheck(pi, spec);
}

runCheckCli(spec);
