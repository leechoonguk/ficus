---
description: Documentation and code comments are English — check-english flags letters from non-Latin scripts outside code; proper nouns go on the allowed list here
globs:
  - "**/*.md"
  - "**/*.go"
  - "**/*.ts"
  - "**/*.sh"
  - "**/*.zsh"
  - "**/*.bash"
  - "**/*.ps1"
  - "**/*.yml"
  - Makefile
alwaysApply: false
---

# English

Documentation (README, docs/, .omp rules, skills, AGENTS.md, RULES.md) and code
comments are written in English. `check-english` (in `make verify`) enforces it.

## What is checked

| File | Checked | Not checked (code) |
|---|---|---|
| Markdown | every line | fenced code blocks, inline code spans |
| Go, TypeScript | `//` and block comments | string, raw-string, template and regex literals |
| Shell, PowerShell, YAML, TOML, Makefile, VHS tape | `#` comments, PowerShell block comments | quoted strings |
| LICENSE | every line | — |

Non-English means a letter from a script other than Latin: Hangul, Kana, Han,
Cyrillic, Greek, … Symbols, box drawing, arrows and accented Latin letters pass.

String literals are code: test data (double-width characters in engine tests) and
words the hooks match in user messages are not comments. Text the program prints
for users is English too, by review rather than by this check.

## Allowed words

Proper nouns that must keep their own script (a product or a name with no Latin
spelling). Each item is a code span, then why; the word then passes anywhere.

(none yet)
