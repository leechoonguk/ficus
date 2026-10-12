import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { registerPostCheck, runCheckCli, type PostCheck } from "../lib/hook-post-check.ts";
import { readText, walkFiles } from "../lib/hook-repo.ts";

/**
 * Check-Omp-Skills Post Hook
 *
 * A skill is discovered by its `SKILL.md` frontmatter (rule://omp-conventions):
 *
 *   1. `.omp/skills/<dir>/SKILL.md` exists for every skill directory.
 *   2. Its frontmatter `name` equals `<dir>` and it has a `description` — the
 *      description is the only text the model sees before deciding to load it.
 *   3. Every hook a skill lists under `enforced-by` exists, so a procedure never
 *      claims a guard that is not there.
 */

export function checkOmpSkills(): string[] {
  const issues: string[] = [];
  const files = walkFiles(".omp/skills");
  const dirs = new Set(files.map((path) => path.split("/")[2]).filter(Boolean));

  for (const dir of dirs) {
    const path = `.omp/skills/${dir}/SKILL.md`;
    const source = readText(path);
    if (source === null) {
      issues.push(`.omp/skills/${dir}/: no SKILL.md — the skill is never discovered`);
      continue;
    }
    const frontmatter = /^---\n([\s\S]*?)\n---\n/.exec(source)?.[1] ?? "";
    const name = /^name:\s*(\S+)/m.exec(frontmatter)?.[1];
    if (name !== dir) issues.push(`${path}: frontmatter name '${name ?? ""}' must equal its directory '${dir}'`);
    if (!/^description:\s*\S/m.test(frontmatter)) issues.push(`${path}: frontmatter has no description`);
    const enforcedBy = /^enforced-by:\s*\[([^\]]*)\]/m.exec(frontmatter)?.[1] ?? "";
    for (const hook of enforcedBy.split(",").map((entry) => entry.trim().replace(/["']/g, "")).filter(Boolean)) {
      const exists = ["pre", "post"].some((kind) => readText(`.omp/hooks/${kind}/${hook}.ts`) !== null);
      if (!exists) issues.push(`${path}: enforced-by lists '${hook}', which is not a hook`);
    }
  }
  return issues;
}

const spec: PostCheck = {
  name: "check-omp-skills",
  check: checkOmpSkills,
  fixHint: "give each .omp/skills/<dir>/SKILL.md a matching name, a description and real enforced-by hooks",
  passMessage: "skills are discoverable and cite real hooks",
  scope: [".omp/skills"],
};

export default function checkOmpSkillsHook(pi: ExtensionAPI): void {
  registerPostCheck(pi, spec);
}

runCheckCli(spec);
