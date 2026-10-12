import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";

/**
 * Releases go through `make release` (rule://release), blocked at the tool boundary.
 *
 * A version tag is the release: pushing `vX.Y.Z` makes GitHub Actions build and
 * publish binaries that `install.sh`, `install.ps1` and every `latest` download
 * pick up within minutes. `make release` checks what a hand-made tag skips — the
 * branch, a clean and pushed tree, a green CI run for that exact commit, a
 * version that follows the last one — and then watches the build and installs
 * the result on three operating systems. So the raw commands that create,
 * move, publish or delete a release are refused, with the target to use instead:
 *
 *   git tag <name> / -a / -d / -f     → make release bump=… | make unrelease v=…
 *   git push … --tags | v1.2.3 | refs/tags/…
 *   gh release create|delete|edit|upload
 *   gh workflow run release.yml
 *   goreleaser release (without --snapshot)
 *
 * Reading stays free: `git tag -l`, bare `git tag`, `gh release view|list`,
 * `goreleaser release --snapshot` (`make dist`), `gh workflow run verify-release.yml`.
 * The user's own terminal is not an agent action and is not gated.
 */

/**
 * Where a command starts. In a shell command, after a separator; in an eval body
 * the command sits inside a string literal (execSync("git tag v1")), so a quote
 * also starts one there — but not in bash, where `echo "git tag is …"` is prose.
 */
const SHELL_START = String.raw`(?:^|[\n&|;()]\s*)(?:sudo\s+)?`;
const EVAL_START = String.raw`(?:^|[\n&|;()"'\x60]\s*)(?:sudo\s+)?`;

function routes(start: string): ReadonlyArray<[RegExp, string]> {
  /** `git tag` with a tag name or a write flag; listing (`-l`, `--list`, bare) passes. */
  const gitTagWrite = new RegExp(
    start + String.raw`git(?:\s+-\S+(?:\s+\S+)?)*\s+tag\s+(?!(?:-l|--list|-n\d*|--contains|--points-at|--sort|--merged|--no-merged)\b)(?:-[adfsu]\b|--(?:annotate|delete|force|sign)\b|[^-\s])`,
  );
  const gitPushTag = new RegExp(
    start + String.raw`git(?:\s+-\S+(?:\s+\S+)?)*\s+push\b[^\n;&|]*(?:--tags|--follow-tags|refs/tags/|(?:^|[\s:+])v\d+\.\d+\.\d+)`,
  );
  const ghReleaseWrite = new RegExp(start + String.raw`gh\s+release\s+(?:create|delete|edit|upload|delete-asset)\b`);
  const ghReleaseWorkflow = new RegExp(start + String.raw`gh\s+workflow\s+run\s+["']?release(?:\.ya?ml)?["']?(?:\s|$)`);
  const goreleaserPublish = new RegExp(start + String.raw`goreleaser\s+release\b(?![^\n;&|]*--snapshot)`);

  return [
    [gitTagWrite, "a raw `git tag` write"],
    [gitPushTag, "pushing a version tag"],
    [ghReleaseWrite, "`gh release` writes"],
    [ghReleaseWorkflow, "starting the release workflow by hand"],
    [goreleaserPublish, "a publishing `goreleaser release`"],
  ];
}

const SHELL_ROUTES = routes(SHELL_START);
const EVAL_ROUTES = routes(EVAL_START);

export default function strictReleaseRoute(pi: ExtensionAPI): void {
  pi.on("tool_call", (event: { toolName?: string; input?: Record<string, unknown> }) => {
    // channel: bash + eval — an eval body can shell out (execSync("git tag v1.2.3")).
    if (event.toolName !== "bash" && event.toolName !== "eval") return;
    const body = String(event.input?.command ?? event.input?.code ?? "");
    for (const [pattern, what] of event.toolName === "eval" ? EVAL_ROUTES : SHELL_ROUTES) {
      if (!pattern.test(body)) continue;
      return {
        block: true,
        reason:
          `release route blocked: ${what} bypasses \`make release\` (rule://release). ` +
          "Release with `make release bump=patch|minor|major`, roll back with " +
          "`make unrelease v=vX.Y.Z` — both only when the user asks in the current turn.",
      };
    }
    return undefined;
  });
}
