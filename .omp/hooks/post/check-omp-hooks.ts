import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
// @ts-expect-error — Node builtin, not project-compiled
import { existsSync } from "node:fs";
// @ts-expect-error — Node builtin, not project-compiled
import { dirname, join, normalize } from "node:path";
import { registerPostCheck, runCheckCli, type PostCheck } from "../lib/hook-post-check.ts";
import { projectRoot, readText, walkFiles } from "../lib/hook-repo.ts";

/**
 * Check-Omp-Hooks Post Hook
 *
 * The harness's own structure (rule://omp-conventions):
 *
 *   1. A post hook registers through `registerPostCheck(pi, spec)` and ends with
 *      `runCheckCli(spec)`, and its `spec.name` is its file name — so it runs
 *      inside `guarded()` and as `make <name>`.
 *   2. A pre hook default-exports a function that handles `tool_call`.
 *   3. Every post hook is listed in config.yml `disabledExtensions` (it runs as a
 *      `make verify` prerequisite, not per tool call), and every listed id names
 *      a hook that exists — a stale id disables nothing and hides the drift.
 *   4. Every relative `.ts` import resolves, and every lib is imported by someone:
 *      an unresolved import kills the hook at load; an orphan lib is dead code.
 */

export function checkOmpHooks(): string[] {
  const issues: string[] = [];
  const files = walkFiles(".omp").filter((path) => path.endsWith(".ts"));
  const config = readText(".omp/config.yml") ?? "";
  const disabled = new Set([...config.matchAll(/^\s*-\s*hook:post:([\w-]+):([\w-]+)\.ts\s*$/gm)].map((m) => m[1]));
  const imported = new Set<string>();

  for (const path of files) {
    const source = readText(path) ?? "";
    const name = /([\w-]+)\.ts$/.exec(path)?.[1] ?? "";

    if (path.startsWith(".omp/hooks/post/")) {
      if (!/\bregisterPostCheck\(pi, spec\)/.test(source) || !/^runCheckCli\(spec\);$/m.test(source)) {
        issues.push(`${path}: must call registerPostCheck(pi, spec) and end with runCheckCli(spec)`);
      }
      if (!new RegExp(`name:\\s*"${name}"`).test(source)) {
        issues.push(`${path}: spec.name must be "${name}" — it is the make target and the warning label`);
      }
      if (!disabled.has(name)) {
        issues.push(`.omp/config.yml: '${name}' is not in disabledExtensions — it would also run on every tool call`);
      }
    }

    if (path.startsWith(".omp/hooks/pre/")) {
      if (!/^export default function \w+\(pi\b/m.test(source) || !source.includes(`"tool_call"`)) {
        issues.push(`${path}: a pre hook default-exports function (pi) and handles "tool_call"`);
      }
    }

    for (const m of source.matchAll(/^import\s[^"']*["'](\.{1,2}\/[^"']+\.ts)["']/gm)) {
      const target = normalize(join(dirname(path), m[1]));
      imported.add(target);
      if (!existsSync(join(projectRoot, target))) issues.push(`${path}: imports ${m[1]}, which does not exist`);
    }
  }

  for (const name of disabled) {
    if (!files.includes(`.omp/hooks/post/${name}.ts`)) {
      issues.push(`.omp/config.yml: disables '${name}', but .omp/hooks/post/${name}.ts does not exist`);
    }
  }
  for (const path of files) {
    // A `.d.ts` is ambient: tsconfig includes it, nothing imports it.
    if (path.startsWith(".omp/hooks/lib/") && !path.endsWith(".d.ts") && !imported.has(path)) {
      issues.push(`${path}: no hook or extension imports it — delete it or wire it`);
    }
  }
  return issues;
}

const spec: PostCheck = {
  name: "check-omp-hooks",
  check: checkOmpHooks,
  fixHint: "follow the post/pre hook shape and keep config.yml disabledExtensions in step with .omp/hooks/post/",
  passMessage: "hook shapes, imports and disabledExtensions agree",
  scope: [".omp/"],
};

export default function checkOmpHooksHook(pi: ExtensionAPI): void {
  registerPostCheck(pi, spec);
}

runCheckCli(spec);
