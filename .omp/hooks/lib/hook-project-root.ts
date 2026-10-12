/**
 * Project root + 트리 순회 제외 — 훅들이 공유하는 사실 (RL 하네스에서 이식).
 */

// @ts-expect-error — Node builtin, not project-compiled
import { existsSync } from "node:fs";
// @ts-expect-error — Node builtin, not project-compiled
import { join } from "node:path";

/** 훅은 프로젝트 루트 또는 .omp/ 안에서 실행된다 — go.mod 의 존재가 루트의 증거다. */
export function resolveProjectRoot(): string {
  const cwd = process.cwd();
  return existsSync(join(cwd, "go.mod")) ? cwd : join(cwd, "..");
}

/** 소스 트리 순회에서 건너뛰는 디렉터리 — 우리가 소유하지 않거나(서브모듈) 생성물이다. */
export const WALK_EXCLUDES: Record<string, true> = {
  ".git": true,
  "third_party": true,
  "bin": true,
  "dist": true,
  "node_modules": true,
};
