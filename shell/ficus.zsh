# ficus: as-you-type completion menu for zsh.
# Load with:  eval "$(ficus init zsh)"   (last in ~/.zshrc, after plugin managers)
#
# The menu is drawn below the command line through POSTDISPLAY and colored
# with region_highlight entries tagged memo=ficus, so zle owns all drawing.
# Typing triggers an asynchronous `ficus complete` (zle -F) so a slow
# completer never blocks input; navigation re-renders synchronously from the
# engine's cache. Every function starts with `emulate -L zsh` so the user's
# options (SH_WORD_SPLIT, KSH_ARRAYS, ...) cannot change its behavior.

# The whole integration is one if-block: a `return` here would return from
# the user's .zshrc that evals this. Unsupported zsh, or a terminal that
# cannot draw a menu (TERM=dumb: Emacs shell, editors' command runners):
# silently nothing.
autoload -Uz is-at-least add-zle-hook-widget
if [[ -o interactive ]] && is-at-least 5.9 && [[ -n $TERM && $TERM != dumb ]]; then

typeset -g __ficus_bin=@FICUS@
@CONFIG@
# Colors the terminal supports, for the theme. A TERM without a terminfo
# entry here (kitty, ghostty over SSH) is assumed to be a modern terminal.
zmodload -F zsh/terminfo +p:terminfo 2>/dev/null
typeset -gi __ficus_colors=${terminfo[colors]:-0}
(( __ficus_colors > 0 )) || __ficus_colors=256
typeset -gi __ficus_n=0 __ficus_sel=0 __ficus_fd=0
typeset -gi __ficus_ghost=0   # length of the ghost text that starts POSTDISPLAY
typeset -gi __ficus_picked=0  # selection moved with Up/Down
typeset -gi __ficus_presel=0  # the engine highlights the best match: Enter takes it
typeset -gi __ficus_held=0    # the engine hides a menu Tab would open
typeset -gi __ficus_manual=0  # Tab opened the menu: show it in full while typing
typeset -g __ficus_move=''    # next-group | prev-group for the next _ficus_run
typeset -g __ficus_post='' __ficus_last=''
typeset -ga __ficus_hl
typeset -gA __ficus_orig      # "keymap:raw key sequence" -> previous widget

# Widgets after which the menu follows the line. Anything else that changes
# the buffer (history, kill-line, ...) closes it.
# A paste (bracketed-paste) is not typing: it closes the menu.
typeset -g __ficus_typing='^(self-insert.*|.*backward-delete-char|delete-char.*|backward-kill-word|magic-space|_ficus_accept|_ficus_forward)$'

# Aliases, functions and builtins are invisible to PATH scanning; hand them
# to the engine for command-position candidates. The file is private to this
# shell (mktemp, not exported): nested shells and tmux panes make their own.
_ficus_export_commands() {
  emulate -L zsh
  local k
  for k in ${(k)aliases}; do print -r -- "$k"$'\t'"alias: ${${aliases[$k]}[1,60]}"; done
  for k in ${(k)functions}; do [[ $k == [_.+-]* ]] || print -r -- "$k"$'\t'"function"; done
  for k in ${(k)builtins}; do print -r -- "$k"$'\t'"builtin"; done
}
_ficus_cleanup() { emulate -L zsh; [[ -n $__ficus_cmds ]] && command rm -f -- $__ficus_cmds }
_ficus_cleanup
typeset -g __ficus_cmds=$(command mktemp "${TMPDIR:-/tmp}/ficus-zsh.XXXXXX" 2>/dev/null)
[[ -n $__ficus_cmds ]] && _ficus_export_commands >| $__ficus_cmds 2>/dev/null
zshexit_functions=(${zshexit_functions:#_ficus_cleanup} _ficus_cleanup)

_ficus_exec() {
  emulate -L zsh
  FICUS_EXTRA_COMMANDS=$__ficus_cmds command $__ficus_bin "$@" 2>/dev/null
}

# Until Up/Down chooses an item (__ficus_picked) the engine highlights the
# first one (__ficus_presel), unless preselect is off.
_ficus_run() {
  emulate -L zsh
  local unselected=(--unselected) manual=() move=() ghost=()
  (( __ficus_picked )) && unselected=()
  (( __ficus_manual )) && manual=(--manual)
  [[ -n $__ficus_move ]] && move=(--move $__ficus_move)
  # zsh-autosuggestions draws its own suggestion in the same place.
  (( __ficus_cfg_ghost && ! $+functions[_zsh_autosuggest_start] )) && ghost=(--ghost)
  _ficus_exec complete --shell zsh --sel $__ficus_sel $unselected $manual $move $ghost --width $COLUMNS --height $LINES \
    --colors $__ficus_colors --theme $__ficus_theme --budget 0 -- "$LBUFFER"
}

_ficus_draw() {
  emulate -L zsh
  region_highlight=( ${region_highlight:#*memo=ficus} )
  POSTDISPLAY=$__ficus_post
  local base=$#BUFFER spec s e
  for spec in $__ficus_hl; do
    s=${spec%% *} spec=${spec#* }
    e=${spec%% *}
    region_highlight+=( "$(( base + s )) $(( base + e )) ${spec#* } memo=ficus" )
  done
}

_ficus_cancel() {
  emulate -L zsh
  (( __ficus_fd )) || return 0
  zle -F $__ficus_fd 2>/dev/null
  exec {__ficus_fd}<&-
  __ficus_fd=0
}

_ficus_reset() {
  emulate -L zsh
  _ficus_cancel
  __ficus_n=0 __ficus_sel=0 __ficus_picked=0 __ficus_presel=0 __ficus_post='' __ficus_hl=() __ficus_held=0 __ficus_manual=0 __ficus_ghost=0
}

_ficus_close() {
  emulate -L zsh
  _ficus_reset
  _ficus_draw
}

# Asynchronous request; the newest one cancels any pending one.
_ficus_request() {
  emulate -L zsh
  _ficus_cancel
  exec {__ficus_fd}< <(_ficus_run)
  zle -F -w $__ficus_fd _ficus_ready
}

_ficus_ready() {
  emulate -L zsh
  local fd=$1 data
  zle -F $fd
  IFS= read -r -d '' -u $fd data
  exec {fd}<&-
  (( fd == __ficus_fd )) || return 0  # superseded or cancelled
  __ficus_fd=0
  [[ -z $2 ]] || return 0             # fd error
  eval "$data"
  _ficus_draw
  zle -R
}

_ficus_sync() {
  emulate -L zsh
  _ficus_cancel
  eval "$(_ficus_run)"
  _ficus_draw
}

# The menu belongs to the end of the line it was computed for.
_ficus_at_end() { (( CURSOR == $#BUFFER )) }

_ficus_pre_redraw() {
  emulate -L zsh
  if [[ $BUFFER == "$__ficus_last" ]]; then
    # Cursor moved away from the end: the menu no longer applies.
    if (( __ficus_n || $#__ficus_post || __ficus_fd )) && ! _ficus_at_end; then
      _ficus_close
    fi
    return
  fi
  __ficus_last=$BUFFER
  if [[ -n $BUFFER && $LASTWIDGET =~ $__ficus_typing ]] && _ficus_at_end &&
     (( __ficus_cfg_auto || __ficus_manual )); then
    __ficus_sel=0 __ficus_picked=0
    # More input is already waiting (a paste without bracketed paste, fast
    # typing): skip the lookup, the last key of the burst makes it.
    if (( PENDING )); then
      _ficus_close
    else
      _ficus_request
    fi
  else
    _ficus_close
  fi
}

# A request still in flight from the previous line must never draw on this
# one, so both ends of a line cancel and reset.
_ficus_line_init() {
  emulate -L zsh
  _ficus_reset
  __ficus_last=$BUFFER
}

_ficus_line_finish() {
  emulate -L zsh
  local shown=$#POSTDISPLAY
  _ficus_close
  # Remove the menu before zle prints the final line, or it stays on screen.
  (( shown )) && zle -R
  return 0
}

# Ctrl-C aborts zle without a final redraw, which would leave the menu on
# screen. Clear it from the INT trap first (via a widget: ZLE variables are
# read-only in traps), then run the user's trap. Loading twice must not
# capture our own trap as the user's.
_ficus_interrupt() { emulate -L zsh; _ficus_close; zle -R }
if [[ $functions[TRAPINT] != *_ficus_interrupt* ]]; then
  if (( $+functions[TRAPINT] )); then
    functions[_ficus_user_trapint]=$functions[TRAPINT]
  else
    # A `trap '...' INT` string; defining TRAPINT replaces it.
    typeset -g __ficus_user_trap=''
    () {
      local line
      for line in ${(f)"$(trap)"}; do
        [[ $line == *' INT' ]] && __ficus_user_trap=${(Q)${(z)line}[3]}
      done
    }
  fi
  TRAPINT() {
    zle && (( __ficus_n || $#__ficus_post || __ficus_fd )) && zle _ficus_interrupt
    if (( $+functions[_ficus_user_trapint] )); then
      _ficus_user_trapint "$@"
      return
    fi
    [[ -n $__ficus_user_trap ]] && eval "$__ficus_user_trap"
    return $(( 128 + $1 ))
  }
fi

# A resize does not run line-pre-redraw: lay the open menu out again for
# the new size from the WINCH trap, then run the user's trap, chained like
# TRAPINT above.
_ficus_resize() { emulate -L zsh; (( $#__ficus_post )) && _ficus_at_end && _ficus_request }
if [[ $functions[TRAPWINCH] != *_ficus_resize* ]]; then
  if (( $+functions[TRAPWINCH] )); then
    functions[_ficus_user_trapwinch]=$functions[TRAPWINCH]
  else
    typeset -g __ficus_user_winch=''
    () {
      local line
      for line in ${(f)"$(trap)"}; do
        [[ $line == *' WINCH' ]] && __ficus_user_winch=${(Q)${(z)line}[3]}
      done
    }
  fi
  TRAPWINCH() {
    zle && (( $#__ficus_post )) && zle _ficus_resize
    if (( $+functions[_ficus_user_trapwinch] )); then
      _ficus_user_trapwinch "$@"
      return
    fi
    [[ -n $__ficus_user_winch ]] && eval "$__ficus_user_winch"
    return 0
  }
fi

# Run what the key did before ficus took it, in the current keymap.
_ficus_fallthrough() {
  emulate -L zsh
  local km=$KEYMAP
  [[ $km == main ]] && km=${${(z)"$(bindkey -lL main)"}[3]}
  local orig=${__ficus_orig[$km:$KEYS]}
  [[ -n $orig ]] && zle $orig
}

# Keys that act on the menu: only while it is open at the end of the line.
_ficus_menu_open() { (( __ficus_n )) && _ficus_at_end }

# With the menu held back (a short command word) or auto_popup off, Tab at
# the end of the line opens it; it then follows typing (__ficus_manual).
_ficus_accept() {
  emulate -L zsh
  _ficus_menu_open || {
    if (( __ficus_held || ! __ficus_cfg_auto )) && [[ -n $BUFFER ]] && _ficus_at_end; then
      __ficus_manual=1 __ficus_sel=0 __ficus_picked=0
      _ficus_sync
      (( __ficus_n )) && return
      __ficus_manual=0
    fi
    _ficus_close
    _ficus_fallthrough
    return
  }
  # With nothing picked, Tab first inserts what all matches share.
  local __ficus_lbuffer='' unselected=(--unselected)
  (( __ficus_picked )) && unselected=()
  eval "$(_ficus_exec accept --shell zsh --sel $__ficus_sel $unselected -- "$LBUFFER")"
  [[ -n $__ficus_lbuffer ]] && LBUFFER=$__ficus_lbuffer
  # pre-redraw sees LASTWIDGET=_ficus_accept and opens the next word's menu.
}

# Right arrow at the end of the line takes the ghost suggestion: the item
# it shows, in full.
_ficus_forward() {
  emulate -L zsh
  if (( __ficus_ghost )) && _ficus_menu_open; then
    local __ficus_lbuffer=''
    eval "$(_ficus_exec accept --shell zsh --sel $__ficus_sel -- "$LBUFFER")"
    [[ -n $__ficus_lbuffer ]] && LBUFFER=$__ficus_lbuffer
    return
  fi
  _ficus_fallthrough
}

# The first Down chooses the first item, the first Up the last one.
_ficus_next() {
  emulate -L zsh
  _ficus_menu_open || { _ficus_close; _ficus_fallthrough; return }
  (( __ficus_picked || __ficus_presel ? __ficus_sel++ : (__ficus_sel = 0) ))
  __ficus_picked=1
  _ficus_sync
}

# Up belongs to history until Down picked an item (up_arrow = history).
_ficus_prev() {
  emulate -L zsh
  if ! _ficus_menu_open || { (( ! __ficus_picked )) && [[ $__ficus_cfg_up != menu ]]; }; then
    _ficus_close
    _ficus_fallthrough
    return
  fi
  (( __ficus_picked || __ficus_presel ? __ficus_sel-- : (__ficus_sel = -1) ))
  __ficus_picked=1
  _ficus_sync
}

# Jump to the first item of the next/previous group (the engine knows them).
_ficus_group() {
  emulate -L zsh
  _ficus_menu_open || { _ficus_close; _ficus_fallthrough; return }
  __ficus_move=$1
  _ficus_sync
  __ficus_move='' __ficus_picked=1
}
_ficus_group_next() { _ficus_group next-group }
_ficus_group_prev() { _ficus_group prev-group }

_ficus_dismiss() {
  emulate -L zsh
  (( $#POSTDISPLAY )) || { _ficus_fallthrough; return }
  _ficus_close
}

# Full-screen fuzzy picker over all candidates (fzf-tab, in-process fzf).
_ficus_pick() {
  emulate -L zsh
  _ficus_close
  local __ficus_lbuffer=''
  zle -I
  eval "$(_ficus_exec pick --shell zsh -- "$LBUFFER" </dev/tty)"
  [[ -n $__ficus_lbuffer ]] && LBUFFER=$__ficus_lbuffer
  __ficus_last=$BUFFER
  zle reset-prompt
}

# Enter with an item highlighted (the first one, or one chosen with
# Up/Down) completes with it and closes the menu: it stays closed until the
# next typed key, so a second Enter runs the line. With no menu (or nothing
# highlighted, preselect off) Enter runs the line exactly as typed.
_ficus_accept_line() {
  emulate -L zsh
  # Enter completes only with a highlighted menu on screen. If a lookup
  # for the last key is still in flight, the menu shown is the previous
  # key's: wait for the current one (the word may now name a command).
  (( __ficus_fd )) && _ficus_menu_open && _ficus_sync
  if _ficus_menu_open && (( __ficus_picked || __ficus_presel )); then
    local __ficus_lbuffer=''
    eval "$(_ficus_exec accept --shell zsh --sel $__ficus_sel -- "$LBUFFER")"
    if [[ -n $__ficus_lbuffer ]]; then
      LBUFFER=$__ficus_lbuffer
      _ficus_close  # pre-redraw keeps it closed: not a typing widget
      return
    fi
  fi
  _ficus_close
  _ficus_fallthrough || zle .accept-line
}

zle -N _ficus_ready
zle -N _ficus_accept
zle -N _ficus_forward
zle -N _ficus_next
zle -N _ficus_prev
zle -N _ficus_dismiss
zle -N _ficus_group_next
zle -N _ficus_group_prev
zle -N _ficus_pick
zle -N _ficus_accept_line
zle -N _ficus_interrupt
zle -N _ficus_resize

# Bind key $1 to widget $2 in the keymaps that follow, remembering per
# keymap what the key did before (never one of our own widgets).
_ficus_bind() {
  emulate -L zsh
  local key=$1 widget=$2 km prev
  shift 2
  for km in "$@"; do
    prev=${${(z)"$(bindkey -M $km -- $key 2>/dev/null)"}[2]}
    if [[ -n $prev && $prev != undefined-key && $prev != _ficus_* ]]; then
      __ficus_orig[$km:${(g:ce:)key}]=$prev
    fi
    bindkey -M $km -- $key $widget
  done
}

for __ficus_k in "${__ficus_key_accept[@]}"; do _ficus_bind "$__ficus_k" _ficus_accept emacs viins; done
_ficus_bind '^[[B' _ficus_next        emacs viins
_ficus_bind '^[OB' _ficus_next        emacs viins
_ficus_bind '^[[A' _ficus_prev        emacs viins
_ficus_bind '^[OA' _ficus_prev        emacs viins
for __ficus_k in "${__ficus_key_pick[@]}"; do _ficus_bind "$__ficus_k" _ficus_pick emacs viins; done
_ficus_bind '^[[C' _ficus_forward     emacs viins
_ficus_bind '^[OC' _ficus_forward     emacs viins
_ficus_bind '^M'   _ficus_accept_line emacs viins
_ficus_bind '^J'   _ficus_accept_line emacs viins
# Esc dismisses in emacs mode only: in vi insert mode it enters command mode.
_ficus_bind '^['   _ficus_dismiss     emacs
for __ficus_k in "${__ficus_key_dismiss[@]}"; do _ficus_bind "$__ficus_k" _ficus_dismiss emacs viins; done
for __ficus_k in "${__ficus_key_next_group[@]}"; do _ficus_bind "$__ficus_k" _ficus_group_next emacs viins; done
for __ficus_k in "${__ficus_key_prev_group[@]}"; do _ficus_bind "$__ficus_k" _ficus_group_prev emacs viins; done
unset __ficus_k
# Escape is also the prefix of Alt-keys and arrows: keep the wait short.
(( KEYTIMEOUT > 10 )) && KEYTIMEOUT=10

add-zle-hook-widget line-pre-redraw _ficus_pre_redraw
add-zle-hook-widget line-init _ficus_line_init
add-zle-hook-widget line-finish _ficus_line_finish

fi
