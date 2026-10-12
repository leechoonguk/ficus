import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { registerPostCheck, runCheckCli, type PostCheck } from "../lib/hook-post-check.ts";
import { readText } from "../lib/hook-repo.ts";

/**
 * Check-Release Post Hook
 *
 * A release is spread over files that name the same things and can drift apart
 * without any build failing (rule://release) — a renamed archive builds fine and
 * breaks every installer the next time `releases/latest` moves:
 *
 *   1. The files `.goreleaser.yaml` publishes (archives per os/arch, Linux
 *      packages, checksums, extra files) are exactly the list `scripts/release.sh`
 *      checks after a release.
 *   2. The installers download what is published: install.sh the
 *      `ficus_<os>_<arch>.tar.gz` archives, install.ps1 the Windows zips,
 *      verify-release.yml the amd64 .deb — and all of them `checksums.txt`.
 *   3. One repository everywhere: GoReleaser's release target, release.sh, both
 *      installers and verify-release.yml.
 *   4. The workflows release.sh drives exist with the triggers it relies on:
 *      ci.yml on pushes to main, release.yml on `v*` tags, verify-release.yml by
 *      hand with a `version` input.
 */

/** A YAML flow list `key: [a, b]` (the form .goreleaser.yaml uses), first match. */
function flowList(yaml: string, key: string): string[] {
  const m = new RegExp(`^\\s*${key}:\\s*\\[([^\\]]*)\\]`, "m").exec(yaml);
  return m === null ? [] : m[1].split(",").map((s) => s.trim()).filter(Boolean);
}

/** What GoReleaser uploads to a release, derived from the config. */
export function publishedAssets(yaml: string): { assets: string[]; issues: string[] } {
  const issues: string[] = [];
  const goos = flowList(yaml, "goos");
  const goarch = flowList(yaml, "goarch");
  if (goos.length === 0 || goarch.length === 0) issues.push(".goreleaser.yaml: no goos/goarch flow lists to derive the archives from");
  const archive = /name_template:\s*"ficus_\{\{ \.Os \}\}_\{\{ \.Arch \}\}"/.test(yaml);
  if (!archive) issues.push('.goreleaser.yaml: archives name_template must stay "ficus_{{ .Os }}_{{ .Arch }}" — the installers build that name');
  const pkg = /file_name_template:\s*"\{\{ \.PackageName \}\}_\{\{ \.Arch \}\}"/.test(yaml);
  if (!pkg) issues.push('.goreleaser.yaml: nfpms file_name_template must stay "{{ .PackageName }}_{{ .Arch }}"');
  if (!/^checksum:\s*\n\s*name_template:\s*checksums\.txt\s*$/m.test(yaml)) {
    issues.push(".goreleaser.yaml: checksum name_template must be checksums.txt — both installers verify against it");
  }

  const assets = ["checksums.txt"];
  for (const os of goos) {
    for (const arch of goarch) assets.push(`ficus_${os}_${arch}.${os === "windows" ? "zip" : "tar.gz"}`);
  }
  if (goos.includes("linux")) {
    for (const format of flowList(yaml.slice(yaml.search(/^nfpms:\s*$/m)), "formats")) {
      for (const arch of goarch) assets.push(`ficus_${arch}.${format}`);
    }
  }
  for (const m of yaml.matchAll(/^\s*-\s*glob:\s*(\S+)\s*$/gm)) assets.push(m[1]);
  return { assets: assets.sort(), issues };
}

export function checkRelease(): string[] {
  const issues: string[] = [];
  const read = (path: string): string => {
    const text = readText(path);
    if (text === null) issues.push(`${path}: not found`);
    return text ?? "";
  };
  const goreleaser = read(".goreleaser.yaml");
  const script = read("scripts/release.sh");
  const installSh = read("install.sh");
  const installPs1 = read("install.ps1");
  const ci = read(".github/workflows/ci.yml");
  const release = read(".github/workflows/release.yml");
  const verify = read(".github/workflows/verify-release.yml");
  if (issues.length > 0) return issues;

  const published = publishedAssets(goreleaser);
  issues.push(...published.issues);
  const listed = /^assets="([^"]*)"/m.exec(script)?.[1].split("\n").map((s) => s.trim()).filter(Boolean).sort() ?? [];
  for (const asset of published.assets) {
    if (!listed.includes(asset)) issues.push(`scripts/release.sh: assets does not list ${asset}, which .goreleaser.yaml publishes`);
  }
  for (const asset of listed) {
    if (!published.assets.includes(asset)) issues.push(`scripts/release.sh: assets lists ${asset}, which .goreleaser.yaml does not publish`);
  }

  if (!installSh.includes("archive=ficus_${os}_${arch}.tar.gz")) issues.push("install.sh: does not download ficus_${os}_${arch}.tar.gz");
  if (!installPs1.includes('$zip = "ficus_windows_$arch.zip"')) issues.push("install.ps1: does not download ficus_windows_$arch.zip");
  if (!verify.includes("ficus_amd64.deb")) issues.push("verify-release.yml: does not install ficus_amd64.deb");
  for (const [path, text] of [["install.sh", installSh], ["install.ps1", installPs1]] as const) {
    if (!text.includes("checksums.txt")) issues.push(`${path}: does not verify against checksums.txt`);
  }

  const owner = /^\s*owner:\s*(\S+)\s*$/m.exec(goreleaser)?.[1];
  const name = /^\s*name:\s*(\S+)\s*$/m.exec(goreleaser.slice(goreleaser.search(/^release:\s*$/m)))?.[1];
  const slug = `${owner}/${name}`;
  for (const [path, text, pattern] of [
    ["scripts/release.sh", script, `repo=${slug}`],
    ["install.sh", installSh, `repo=${slug}`],
    ["install.ps1", installPs1, `$repo = '${slug}'`],
    [".github/workflows/verify-release.yml", verify, `github.com/${slug}/releases`],
  ] as const) {
    if (!text.includes(pattern)) issues.push(`${path}: does not use the repository ${slug} that .goreleaser.yaml releases to`);
  }

  if (!/^name:\s*ci\s*$/m.test(ci) || !/push:\s*\n\s*branches:\s*\[main\]/.test(ci)) {
    issues.push(".github/workflows/ci.yml: must be named ci and run on pushes to main — release.sh requires its green run");
  }
  if (!/tags:\s*\["v\*"\]/.test(release) || !release.includes("goreleaser/goreleaser-action")) {
    issues.push('.github/workflows/release.yml: must run GoReleaser on tags ["v*"] — a pushed version tag is the release');
  }
  if (!/workflow_dispatch:\s*\n\s*inputs:\s*\n\s*version:/.test(verify)) {
    issues.push(".github/workflows/verify-release.yml: needs workflow_dispatch with a version input — release.sh starts it per version");
  }
  return issues;
}

const spec: PostCheck = {
  name: "check-release",
  check: checkRelease,
  fixHint: "keep .goreleaser.yaml, scripts/release.sh, the installers and the workflows naming the same files and repository",
  passMessage: "release config, release script, installers and workflows agree",
  scope: [".goreleaser.yaml", "scripts/release.sh", "install.sh", "install.ps1", ".github/workflows/"],
};

export default function checkReleaseHook(pi: ExtensionAPI): void {
  registerPostCheck(pi, spec);
}

runCheckCli(spec);
