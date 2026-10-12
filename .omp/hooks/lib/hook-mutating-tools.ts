/**
 * The one list of editing tools that write files.
 *
 * 48 post hooks once kept their own copies of this list, and all of them broke
 * when pi-hashline-edit-pro arrived (measured: the plugin's replace/insert edits
 * bypassed every check trigger). A tool that writes files, built-in or plugin,
 * is added here and nowhere else.
 */
export const FILE_EDIT_TOOLS = ["write", "edit", "replace", "insert", "undo_last_change"] as const; // the last three: pi-hashline-edit-pro (linked into omp globally)

/** Every tool that can change files or state — shared by the post hooks' read-only filter. */
export const MUTATING_TOOLS = new Set<string>([...FILE_EDIT_TOOLS, "bash", "eval", "ast_edit", "lsp", "browser", "hub", "task"]);

/** Without bash — for hooks whose gate itself runs a shell and would fire recursively (check-verify). */
export const MUTATING_TOOLS_NO_BASH = new Set<string>([...FILE_EDIT_TOOLS, "eval", "ast_edit", "lsp", "browser", "hub", "task"]);
