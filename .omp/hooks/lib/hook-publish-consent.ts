/**
 * Did the user ask for this publish?
 *
 * `make verify` proves the tree is green; it says nothing about whether anyone
 * wanted the tree published. This module answers the second question from the
 * session's own entries, so a commit the agent decided to make on its own is
 * blocked before it runs.
 *
 * The rule is the **current** user turn, not the session: a request to commit
 * three turns ago, followed by other instructions, is not permission to publish
 * now. And a turn that names the verb while forbidding it is not permission
 * either - the message that created this gate reads "커밋하고 푸쉬 하지말도록",
 * which a word match alone would have read as consent.
 */

/** A session entry as `sessionManager.getBranch()` yields it. */
export interface BranchEntry {
  type?: string;
  role?: string;
  content?: unknown;
  message?: { role?: string; content?: unknown };
}

/** Publishing verbs, in both languages this repository is worked in. */
const CONSENT = /커밋|푸시|푸쉬|배포|올려|\bcommit\b|\bpush\b|\bpublish\b/i;

/**
 * Release verbs. A release is a separate, larger act than a push: a version tag
 * builds and publishes binaries every user downloads. "커밋해" is not a request
 * for one, so a release needs its own words in the current turn.
 */
const RELEASE_CONSENT = /릴리스|릴리즈|출시|배포|버전\s*(?:을|를)?\s*(?:올|업)|태그|\brelease\b|\btag\b/i;

/**
 * Negation in the same message. Korean puts it after the verb (`하지마`, `말고`),
 * English before it (`don't push`), so this is a message-level veto rather than a
 * neighbourhood test: a turn that both asks and forbids is ambiguous, and a
 * blocked publish is the cheap side of that ambiguity.
 */
const NEGATION =
  /하지\s*말|하지마|말도록|말고|마세요|않도록|없이|금지|안\s*됨|안돼|안\s*하|\bdon'?t\b|\bdo not\b|\bnever\b|\bwithout\b|\bstop\b/i;

/** System-authored blocks ride inside user messages; they cannot grant consent. */
const SYSTEM_BLOCK = /<(system-reminder|system-notice|system-directive|continuity[^>]*)>[\s\S]*?<\/\1>/gi;

/** Text of one entry, when it is a user message. */
function userText(entry: BranchEntry): string | null {
  const role = entry.role ?? entry.message?.role;
  // domain-literal: a transcript message role, not the `user` CRUD domain
  if (role !== "user") return null;
  const content = entry.content ?? entry.message?.content;
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return null;
  const text = content
    .filter((part): part is { type?: string; text?: string } => typeof part === "object" && part !== null)
    .map((part) => (part.type === undefined || part.type === "text" ? String(part.text ?? "") : ""))
    .join("\n");
  return text === "" ? null : text;
}

/** The newest user turn, stripped of anything the system wrote inside it. */
export function latestUserTurn(entries: readonly BranchEntry[]): string | null {
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    const text = userText(entries[index]);
    if (text === null) continue;
    const own = text.replace(SYSTEM_BLOCK, " ").trim();
    if (own === "") continue;
    return own;
  }
  return null;
}

export type ConsentVerdict = { granted: true } | { granted: false; reason: string };

/**
 * Consent for one publish attempt.
 *
 * Fail closed: no readable user turn means no permission, because the alternative
 * is publishing on an assumption.
 */
export function publishConsent(entries: readonly BranchEntry[]): ConsentVerdict {
  return consent(entries, CONSENT, "a commit or a push");
}

/** Consent for a release (`make release`) or its rollback (`make unrelease`). */
export function releaseConsent(entries: readonly BranchEntry[]): ConsentVerdict {
  return consent(entries, RELEASE_CONSENT, "a release");
}

function consent(entries: readonly BranchEntry[], verbs: RegExp, what: string): ConsentVerdict {
  const turn = latestUserTurn(entries);
  if (turn === null) {
    return { granted: false, reason: "no user turn is readable in this session" };
  }
  if (!verbs.test(turn)) {
    return { granted: false, reason: `the current user turn does not ask for ${what}` };
  }
  if (NEGATION.test(turn)) {
    return {
      granted: false,
      reason: `the current user turn names ${what} but negates it, so it is not a request`,
    };
  }
  return { granted: true };
}
