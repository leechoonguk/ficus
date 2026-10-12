/**
 * Hook scope — 훅은 자기 도메인 경로에만 발화한다.
 *
 * 실측(프롬프트 단위 프로브): 변이 도구 결과 하나에 post 훅 36개가 붙어
 * write 6.8초 / bash 4.9초를 썼고, 세션 벽시계의 22~36%가 도구 구간이었다. 비용의 절반은
 * 이벤트와 무관한 하네스 메타 검사(.omp/ 구조·타입·덱)였다 — backend 한 줄을 고쳐도 돌았다.
 *
 * `touchesPath` 는 경로가 있는 이벤트(write/edit)만 걸러내고 셸은 통과시킨다(명령이 만든
 * 파일까지 놓치지 않기 위해서다). `touchesStrict` 는 셸 명령 문자열까지 본다 — 자기 도메인이
 * 명령에 등장하지 않으면 건너뛴다. 하네스 메타 검사처럼 대상 경로가 좁고 확실할 때만 쓴다.
 */

type HookEvent = { toolName?: string; input?: Record<string, unknown> };

const SHELL_TOOLS = new Set(["bash", "eval"]);

function pathOf(event: HookEvent): string {
  const i = event.input ?? {};
  return String(i.path ?? i.file_path ?? i.filePath ?? i.target ?? "");
}

function commandOf(event: HookEvent): string {
  const i = event.input ?? {};
  return String(i.command ?? i.code ?? "");
}

/** 경로를 아는 이벤트만 거른다. 셸·경로 없는 이벤트는 항상 통과(보수적). */
export function touchesPath(event: HookEvent, prefixes: string[]): boolean {
  const p = pathOf(event);
  if (!p) return true;
  return prefixes.some((pre) => p.includes(pre));
}

/** 셸 명령 문자열까지 본다. 도메인이 어디에도 안 보이면 건너뛴다. */
export function touchesStrict(event: HookEvent, prefixes: string[]): boolean {
  const p = pathOf(event);
  if (p) return prefixes.some((pre) => p.includes(pre));
  if (SHELL_TOOLS.has(String(event.toolName ?? ""))) {
    const c = commandOf(event);
    if (!c) return true;
    return prefixes.some((pre) => c.includes(pre));
  }
  return true;
}
