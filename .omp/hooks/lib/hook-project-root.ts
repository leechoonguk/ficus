/**
 * Project root and tree-walk exclusions — facts every hook shares (ported from
 * the RL harness).
 */

// @ts-expect-error — Node builtin, not project-compiled
import { existsSync } from "node:fs";
// @ts-expect-error — Node builtin, not project-compiled
import { join } from "node:path";

/** Hooks run from the project root or from .omp/ — go.mod marks the root. */
export function resolveProjectRoot(): string {
  const cwd = process.cwd();
  return existsSync(join(cwd, "go.mod")) ? cwd : join(cwd, "..");
}

/** Directories a tree walk skips — not ours (submodules, the Go toolchain
 * scripts/go.sh downloads into .cache/) or generated. */
export const WALK_EXCLUDES: Record<string, true> = {
  ".git": true,
  ".cache": true,
  "third_party": true,
  "bin": true,
  "dist": true,
  "node_modules": true,
};
