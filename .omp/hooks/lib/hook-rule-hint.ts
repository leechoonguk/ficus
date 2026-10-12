/**
 * Rule hint — shared by every check's CLI tail (and hook-safety-guard) to name
 * the rule a failing check enforces, so the agent can `rule://` it instead of
 * rediscovering requirements one failure at a time.
 */
// @ts-expect-error — Node builtin, not project-compiled
import { existsSync, readFileSync } from "node:fs";
// @ts-expect-error — Node builtin, not project-compiled
import { join } from "node:path";

/** Print the mapped rule (name + frontmatter description) for a failing check. */
export function printRuleHint(hookName: string): void {
  const rule = ruleForHook(hookName);
  if (!rule) return;
  const desc = ruleDescription(rule);
  console.log(`    Rule: rule://${rule}${desc ? ` — ${desc}` : ""}`);
}

/** check-<x> hook name → rulebook rule name. Only non-1:1 names are listed;
 *  existence is verified so a wrong guess prints nothing (no misattribution). */
export function ruleForHook(hookName: string): string | null {
  const base = hookName.replace(/^check-/, "");
  const candidate = RULE_NAME_EXCEPTIONS[base] ?? base;
  const path = join(process.cwd(), ".omp", "rules", `${candidate}.md`);
  try { if (existsSync(path)) return candidate; } catch { /* outside project cwd — omit */ }
  return null;
}

const RULE_NAME_EXCEPTIONS: Record<string, string> = {
  "omp-hooks": "omp-conventions",
  "omp-rules": "omp-conventions",
  "omp-skills": "omp-conventions",
  "makefiles": "root-makefile",
};

function ruleDescription(rule: string): string | null {
  try {
    const content = readFileSync(join(process.cwd(), ".omp", "rules", `${rule}.md`), "utf-8");
    const m = content.match(/^description:\s*(.+)$/m);
    return m ? m[1].trim() : null;
  } catch { return null; }
}
