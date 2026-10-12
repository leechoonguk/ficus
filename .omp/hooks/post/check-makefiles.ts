import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { registerPostCheck, runCheckCli, type PostCheck } from "../lib/hook-post-check.ts";
import { parseMakefile, phonyTargets, readText, walkFiles } from "../lib/hook-repo.ts";

/**
 * Check-Makefiles Post Hook
 *
 * The Makefile is the only index of what can be run (rule://root-makefile):
 *
 *   1. Every target has a `## help` line, so `make help` lists it.
 *   2. `.PHONY` names exactly the targets — none of them produce a file.
 *   3. No catch-all rule (`%:` / `.DEFAULT:`): it answers an unknown target with
 *      success, so a typo'd `check-*` gate would pass without running.
 *   4. Every `.omp/hooks/post/check-<x>.ts` has a `check-<x>` target that runs it,
 *      and that target is a prerequisite of `verify` — a check outside the gate is
 *      never run.
 *   5. root-makefile.md documents exactly the Makefile's targets.
 *   6. Every `make <target>` written in the harness (.omp/**) names a real target —
 *      an instruction that cannot be executed is worse than none.
 */

/** Words that read like targets in prose but name a pattern, not a command. */
const PLACEHOLDERS: Record<string, true> = { "check-": true, "check-x": true, target: true, "<target>": true };

export function checkMakefiles(): string[] {
  const text = readText("Makefile");
  if (text === null) return ["Makefile: not found"];
  const targets = parseMakefile(text);
  const issues: string[] = [];

  for (const target of targets.values()) {
    if (target.help === null || target.help === "") {
      issues.push(`Makefile: target '${target.name}' has no '## <description>' — make help cannot list it`);
    }
  }

  const phony = phonyTargets(text);
  for (const name of targets.keys()) {
    if (!phony.includes(name)) issues.push(`Makefile: target '${name}' missing from .PHONY`);
  }
  for (const name of phony) {
    if (!targets.has(name)) issues.push(`Makefile: .PHONY lists '${name}', which is not a target`);
  }

  for (const line of text.split("\n")) {
    if (/^(%[^\s:=]*|\.DEFAULT)\s*:(?!=)/.test(line)) {
      issues.push(`Makefile: catch-all rule '${line.trim()}' — every unknown target would succeed`);
    }
  }

  const verify = targets.get("verify");
  for (const path of walkFiles(".omp/hooks/post")) {
    const name = /([\w-]+)\.ts$/.exec(path)?.[1];
    if (name === undefined) continue;
    const target = targets.get(name);
    if (target === undefined) {
      issues.push(`${path}: no '${name}' make target`);
      continue;
    }
    if (!target.recipe.some((line) => line.includes(`node ${path}`))) {
      issues.push(`Makefile ${name}: recipe does not run 'node ${path}'`);
    }
    if (verify === undefined || !verify.prerequisites.includes(name)) {
      issues.push(`Makefile: '${name}' is not a prerequisite of verify — a check outside the gate never runs`);
    }
  }

  issues.push(...documentedTargets(new Set(targets.keys())));
  issues.push(...harnessReferences(new Set(targets.keys())));
  return issues;
}

/** root-makefile.md's target table (first column, backticked `make <target>`) both ways. */
function documentedTargets(actual: Set<string>): string[] {
  const rule = readText(".omp/rules/root-makefile.md");
  if (rule === null) return [".omp/rules/root-makefile.md: not found — the target table lives there"];
  const documented = new Set([...rule.matchAll(/^\|\s*`make ([\w-]+)[^`]*`\s*\|/gm)].map((m) => m[1]));
  const issues: string[] = [];
  for (const name of actual) {
    if (!documented.has(name)) issues.push(`root-makefile.md: target '${name}' is not documented`);
  }
  for (const name of documented) {
    if (!actual.has(name)) issues.push(`root-makefile.md: documents 'make ${name}', which is not a target`);
  }
  return issues;
}

/** `make <target>` mentioned anywhere in the harness must be runnable. */
function harnessReferences(actual: Set<string>): string[] {
  const issues: string[] = [];
  for (const path of walkFiles(".omp")) {
    if (!/\.(md|ts)$/.test(path) || path.startsWith(".omp/state/")) continue;
    const source = readText(path) ?? "";
    for (const m of source.matchAll(/`make ([a-z][\w-]*)/g)) {
      if (PLACEHOLDERS[m[1]] === true || actual.has(m[1])) continue;
      issues.push(`${path}: refers to \`make ${m[1]}\`, which is not a target`);
    }
  }
  return issues;
}

const spec: PostCheck = {
  name: "check-makefiles",
  check: checkMakefiles,
  fixHint: "keep Makefile targets, .PHONY, verify prerequisites and root-makefile.md in step",
  passMessage: "Makefile targets, verify membership and docs agree",
  scope: ["Makefile", ".omp/"],
};

export default function checkMakefilesHook(pi: ExtensionAPI): void {
  registerPostCheck(pi, spec);
}

runCheckCli(spec);
