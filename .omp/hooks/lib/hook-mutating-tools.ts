/**
 * 파일을 쓰는 편집 툴의 단일 목록.
 *
 * post 훅 48곳이 같은 목록을 각자 들고 있다가 pi-hashline-edit-pro 도입에서 전부
 * 구멍났다(실측: 플러그인 replace/insert 편집이 검사 트리거를 우회했다).
 * 빌트인이든 플러그인이든 "파일을 쓰는 툴"은 여기 한 곳에만 추가한다.
 */
export const FILE_EDIT_TOOLS = ["write", "edit", "replace", "insert", "undo_last_change"] as const; // 뒤 셋 = pi-hashline-edit-pro(omp 전역 링크 — 두 트리 공통)

/** 파일·상태를 바꿀 수 있는 툴 전체 — post 훅의 read-only 필터가 공유한다. */
export const MUTATING_TOOLS = new Set<string>([...FILE_EDIT_TOOLS, "bash", "eval", "ast_edit", "lsp", "browser", "hub", "task"]);

/** bash 제외판 — check-verify 처럼 게이트 자신이 셸을 돌려 재귀 발화하는 훅용. */
export const MUTATING_TOOLS_NO_BASH = new Set<string>([...FILE_EDIT_TOOLS, "eval", "ast_edit", "lsp", "browser", "hub", "task"]);
