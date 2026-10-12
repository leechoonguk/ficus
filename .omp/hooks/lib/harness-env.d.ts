/**
 * Ambient Harness Environment — ported from fast-api-standard.
 *
 * The harness runs on `node` with no package.json and no `@types/node`: hooks are
 * executed directly (`node .omp/hooks/post/<name>.ts`), so a dependency added for
 * type-checking alone would put a node_modules tree under a tree that has none.
 * The globals the hooks call are declared here, once, with the host module they
 * import. Node builtins (`node:fs`, `node:path`, ...) stay behind their
 * `@ts-expect-error` markers: the runtime resolves them.
 */

declare const console: {
  log(...args: unknown[]): void;
  error(...args: unknown[]): void;
};

declare const process: {
  argv: string[];
  env: Record<string, string | undefined>;
  cwd(): string;
  exit(code?: number): never;
};

/**
 * Only the members the hooks and extensions call. `on` is a method on purpose:
 * method parameters are bivariant, so a handler typed for its own event shape
 * still assigns.
 */
interface HostExtensionApi {
  on(event: string, handler: (event: never, ctx: never) => unknown): void;
  sendUserMessage?(message: string): void;
}

/** The package itself is resolved by the OMP runtime, not by this project. */
declare module "@oh-my-pi/pi-coding-agent" {
  export type ExtensionAPI = HostExtensionApi;
}
