# ficus: as-you-type completion menu for bash (>= 4.0).
# Load with:  eval "$(ficus init bash)"   (last in ~/.bashrc)
#
# Readline has no hook after each edit, so printable keys and Backspace are
# bound with `bind -x` to functions that edit READLINE_LINE themselves and
# then redraw the menu. The menu is painted below the command line with
# cursor-relative escape sequences; readline only repaints its own line.
#
# Keys are re-bound with the state instead of dispatching at run time, so
# readline always sees the real function of a key:
#   menu open    Tab, Up, Down, Ctrl-G run ficus actions
#   menu closed  they get back exactly what they had before (function, macro
#                or bind -x command), so e.g. a repeated Tab still lists
#   Ctrl-R/S     Backspace returns to backward-delete-char for the search
#                (isearch only honors a key bound directly to it) and is
#                taken back by the next typed character

# The whole integration is one if-block: a top-level `return` here would
# fail inside eval, or return from the user's .bashrc. .bashrc is shared by
# every bash on the machine (macOS /bin/bash is 3.2): unsupported versions
# silently skip it, as do terminals that cannot draw a menu (TERM=dumb).
if [[ $- == *i* ]] && (( BASH_VERSINFO[0] >= 4 )) && [[ -n ${TERM-} && $TERM != dumb ]]; then

__ficus_bin=@FICUS@
@CONFIG@
# Colors the terminal supports, for the theme. A TERM without a terminfo
# entry here (kitty, ghostty over SSH) is assumed to be a modern terminal.
__ficus_colors=$(tput colors 2>/dev/null)
[[ $__ficus_colors =~ ^[0-9]+$ ]] && (( __ficus_colors > 0 )) || __ficus_colors=256
__ficus_n=0 __ficus_sel=0 __ficus_rows=0 __ficus_menu=''
__ficus_picked=0      # selection moved with Up/Down
__ficus_presel=0      # the engine highlights the best match: Enter takes it
__ficus_held=0        # the engine hides a menu Tab would open
__ficus_manual=0      # Tab opened the menu: show it in full while typing
__ficus_move=''       # next-group | prev-group for the next query
__ficus_shown_lines=() __ficus_shown_endrow=0 __ficus_shown_cols=0  # frame on screen
__ficus_drawn=0       # menu rows currently on screen
__ficus_measured=0   # __ficus_ptext holds this line's prompt
__ficus_ptext=''      # visible text of the prompt's last line
__ficus_endrow=0      # screen row the line ends on (from the engine)
__ficus_for=''        # the line the open menu was computed for
__ficus_keys=plain    # which set of menu keys is bound: plain | menu
__ficus_armed=0       # Backspace bound to ficus

# Aliases, functions and builtins are invisible to PATH scanning; hand them
# to the engine. The file is private to this shell (mktemp, not exported).
[[ -n ${__ficus_cmds-} ]] && command rm -f -- "$__ficus_cmds"
__ficus_cmds=$(command mktemp "${TMPDIR:-/tmp}/ficus-bash.XXXXXX" 2>/dev/null)
if [[ -n $__ficus_cmds ]]; then
  {
    compgen -A alias | while IFS= read -r k; do printf '%s\talias\n' "$k"; done
    compgen -A function | while IFS= read -r k; do [[ $k == _* ]] || printf '%s\tfunction\n' "$k"; done
    compgen -A builtin | while IFS= read -r k; do printf '%s\tbuiltin\n' "$k"; done
  } >| "$__ficus_cmds" 2>/dev/null
  # Chain the user's EXIT trap instead of replacing it.
  __ficus_exit_prev=$(trap -p EXIT)
  __ficus_exit_prev=${__ficus_exit_prev#trap -- \'}
  __ficus_exit_prev=${__ficus_exit_prev%\' EXIT}
  __ficus_exit_prev=${__ficus_exit_prev//\'\\\'\'/\'}
  if [[ $__ficus_exit_prev != *__ficus_cmds* ]]; then
    trap "command rm -f -- \"\$__ficus_cmds\"${__ficus_exit_prev:+; $__ficus_exit_prev}" EXIT
  fi
fi

__ficus_exec() {
  FICUS_EXTRA_COMMANDS=$__ficus_cmds command "$__ficus_bin" "$@" 2>/dev/null
}

# Visible text of the prompt's last line, once per command line, from
# ${PS1@P} with the \[...\] (\001...\002) invisible parts removed. The engine
# measures it (wide characters included) to know where the line ends. A
# cursor position report would race with typed-ahead keys and cost a round
# trip over SSH.
__ficus_measure() {
  (( __ficus_measured )) && return
  local p
  if (( BASH_VERSINFO[0] > 4 || BASH_VERSINFO[1] >= 4 )); then
    p=${PS1@P}
  else
    p=$PS1
  fi
  p=${p##*$'\n'}
  while [[ $p == *$'\001'*$'\002'* ]]; do
    p=${p%%$'\001'*}${p#*$'\002'}
  done
  __ficus_ptext=$p __ficus_measured=1
}

# Paint (or erase, with no menu) below the line.
#
# bash runs a bind -x command after rl_clear_visible_line(), which leaves
# the cursor at column 0 of the line's first row, and afterwards repaints the
# line from there (rl_redraw_prompt_last_line). So painting starts at that
# origin and must end there. Newlines (not cursor-down) are used to move
# down so the screen scrolls when the menu does not fit below.
#
# When the previous frame has the same geometry only the rows that changed
# are rewritten, and every frame is a synchronized update (DEC mode 2026)
# so terminals that support it never show a half-drawn menu.
__ficus_paint() {
  local out=$'\e[?2026h' i n up cols=${COLUMNS:-80}
  local -a lines=()
  (( __ficus_rows )) && mapfile -t lines <<<"$__ficus_menu"
  n=${#lines[@]}
  if (( n == 0 )); then
    (( __ficus_drawn )) && printf '%s' "$out"$'\n\r\e[J\e[A\e[?2026l' >/dev/tty
    __ficus_drawn=0 __ficus_shown_lines=()
  elif (( ! __ficus_drawn || __ficus_endrow != __ficus_shown_endrow || cols != __ficus_shown_cols )); then
    out+=$'\n\r\e[J'
    for (( i = 0; i < __ficus_endrow; i++ )); do out+=$'\n'; done
    out+="${__ficus_menu//$'\n'/$'\r\n'}"$'\r\e['"$(( __ficus_endrow + n ))"A$'\e[?2026l'
    printf '%s' "$out" >/dev/tty
  else
    for (( i = 0; i <= __ficus_endrow; i++ )); do out+=$'\n'; done
    out+=$'\r'
    for (( i = 0; i < n; i++ )); do
      (( i )) && out+=$'\n'
      [[ ${lines[i]} == "${__ficus_shown_lines[i]-}" ]] || out+=$'\e[2K'"${lines[i]}"$'\r'
    done
    up=$(( __ficus_endrow + n ))
    if (( __ficus_drawn > n )); then
      out+=$'\n\e[J'  # the old frame was taller
      (( up++ ))
    fi
    printf '%s' "$out"$'\e['"$up"A$'\e[?2026l' >/dev/tty
  fi
  if (( n )); then
    __ficus_shown_lines=("${lines[@]}") __ficus_shown_endrow=$__ficus_endrow __ficus_shown_cols=$cols
    __ficus_drawn=$n
  fi
  if __ficus_menu_valid; then
    __ficus_bind_menu_keys
  elif (( __ficus_held || ! __ficus_cfg_auto )) && [[ -n $READLINE_LINE ]]; then
    __ficus_bind_accept_only  # Tab opens the menu
  else
    __ficus_bind_plain_keys
  fi
}

# The menu applies only to the line it was computed for, with the cursor at
# its end: keys ficus does not see (Ctrl-P, Delete, Alt-., ...) can change
# the line or move the cursor while it is open.
__ficus_menu_valid() {
  (( __ficus_n )) && [[ $READLINE_LINE == "$__ficus_for" ]] && __ficus_at_end
}

__ficus_at_end() { (( READLINE_POINT == ${#READLINE_LINE} )); }

# Until Up/Down chooses an item (__ficus_picked) the engine highlights the
# first one (__ficus_presel), unless preselect is off.
__ficus_query() {
  __ficus_measure
  local unselected=--unselected manual= move=
  (( __ficus_picked )) && unselected=
  (( __ficus_manual )) && manual=--manual
  [[ -n $__ficus_move ]] && move="--move=$__ficus_move"
  eval "$(__ficus_exec complete --shell bash --sel "$__ficus_sel" $unselected $manual $move --width "${COLUMNS:-80}" \
    --height "${LINES:-24}" --colors "$__ficus_colors" --theme "$__ficus_theme" --budget "$__ficus_budget" \
    --prompt "$__ficus_ptext" -- "${READLINE_LINE:0:READLINE_POINT}")"
  __ficus_for=$READLINE_LINE
}

__ficus_close() {
  __ficus_n=0 __ficus_sel=0 __ficus_picked=0 __ficus_presel=0 __ficus_held=0 __ficus_rows=0 __ficus_menu=''
  __ficus_paint
}

# Refresh after an edit. The menu follows the cursor only at the line end,
# and with auto_popup off only once Tab opened it (__ficus_manual).
__ficus_update() {
  __ficus_sel=0 __ficus_picked=0
  [[ -n $READLINE_LINE ]] || __ficus_manual=0
  if [[ -z $READLINE_LINE ]] || ! __ficus_at_end || (( ! __ficus_cfg_auto && ! __ficus_manual )); then
    __ficus_close
    return
  fi
  # More input is already waiting (a paste without bracketed paste, fast
  # typing): skip the lookup, the last key of the burst makes it.
  if read -t 0 </dev/tty 2>/dev/null; then
    __ficus_close
    return
  fi
  __ficus_query
  __ficus_paint
}

__ficus_insert() {
  (( __ficus_armed )) || __ficus_arm
  READLINE_LINE=${READLINE_LINE:0:READLINE_POINT}$1${READLINE_LINE:READLINE_POINT}
  (( READLINE_POINT += ${#1} ))
  __ficus_update
}

__ficus_backspace() {
  (( READLINE_POINT > 0 )) || return 0
  READLINE_LINE=${READLINE_LINE:0:READLINE_POINT-1}${READLINE_LINE:READLINE_POINT}
  (( READLINE_POINT-- ))
  __ficus_update
}

# With nothing picked, Tab first inserts what all matches share.
__ficus_accept() {
  local __ficus_lbuffer='' unselected=--unselected
  (( __ficus_picked )) && unselected=
  eval "$(__ficus_exec accept --shell bash --sel "$__ficus_sel" $unselected -- "${READLINE_LINE:0:READLINE_POINT}")"
  [[ -n $__ficus_lbuffer ]] || return 0
  READLINE_LINE=$__ficus_lbuffer${READLINE_LINE:READLINE_POINT}
  READLINE_POINT=${#__ficus_lbuffer}
  __ficus_update
}
# The first Down chooses the first item (the second when the first is
# preselected), Up the last one.
__ficus_next() { (( __ficus_picked || __ficus_presel ? __ficus_sel++ : (__ficus_sel = 0) )); __ficus_picked=1; __ficus_query; __ficus_paint; }
__ficus_prev() { (( __ficus_picked || __ficus_presel ? __ficus_sel-- : (__ficus_sel = -1) )); __ficus_picked=1; __ficus_query; __ficus_paint; }
__ficus_dismiss() { __ficus_close; }
# Jump to the first item of the next/previous group (the engine knows them).
__ficus_group() { __ficus_move=$1; __ficus_query; __ficus_move= __ficus_picked=1; __ficus_paint; }
__ficus_group_next() { __ficus_group next-group; }
__ficus_group_prev() { __ficus_group prev-group; }

# Enter with an item highlighted (the first one, or one chosen with
# Up/Down) completes with it and the line does not run: the second step of
# the Enter macro (k<id>, the key's original action) becomes a no-op. The
# menu stays closed until the next typed key, so a second Enter runs the
# line. With no menu (or nothing highlighted, preselect off) the line runs
# exactly as typed.
__ficus_enter() {
  local km id
  if __ficus_menu_valid && (( __ficus_picked || __ficus_presel )); then
    local __ficus_lbuffer=''
    eval "$(__ficus_exec accept --shell bash --sel "$__ficus_sel" -- "$READLINE_LINE")"
    if [[ -n $__ficus_lbuffer ]]; then
      for km in "${__ficus_kms[@]}"; do
        for id in m j; do bind -m "$km" -x "\"\\C-x\\C-]k$id\": :"; done
      done
      READLINE_LINE=$__ficus_lbuffer
      READLINE_POINT=${#READLINE_LINE}
      __ficus_close
      return
    fi
  fi
  for km in "${__ficus_kms[@]}"; do
    __ficus_bind_action "$km" '\C-x\C-]km' "${__ficus_orig[$km|\\C-m]}"
    __ficus_bind_action "$km" '\C-x\C-]kj' "${__ficus_orig[$km|\\C-j]}"
  done
  __ficus_close
}

# Incremental search is about to start: give Backspace back to readline.
__ficus_search() {
  __ficus_close
  __ficus_disarm
}

__ficus_pick() {
  local __ficus_lbuffer=''
  __ficus_close
  eval "$(__ficus_exec pick --shell bash -- "${READLINE_LINE:0:READLINE_POINT}" </dev/tty)"
  [[ -n $__ficus_lbuffer ]] || return 0
  READLINE_LINE=$__ficus_lbuffer${READLINE_LINE:READLINE_POINT}
  READLINE_POINT=${#__ficus_lbuffer}
}

# After every command: forget the measured column, wipe a menu the previous
# line left on screen (Ctrl-C aborts without running our hooks) and take the
# keys back. Appended to PROMPT_COMMAND and returning the status it got, so
# hooks and prompts that show $? / PIPESTATUS are unaffected.
__ficus_prompt() {
  local status=$?
  (( __ficus_drawn )) && printf '\r\e[J' >/dev/tty
  __ficus_n=0 __ficus_sel=0 __ficus_rows=0 __ficus_drawn=0 __ficus_measured=0 __ficus_held=0 __ficus_manual=0
  __ficus_bind_plain_keys
  (( __ficus_armed )) || __ficus_arm
  return $status
}
if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == "declare -a"* ]]; then
  [[ " ${PROMPT_COMMAND[*]} " == *" __ficus_prompt "* ]] || PROMPT_COMMAND+=(__ficus_prompt)
elif [[ ${PROMPT_COMMAND-} != *__ficus_prompt* ]]; then
  PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND; }__ficus_prompt"
fi

# --- key bindings -----------------------------------------------------------
#
# Every binding names its keymap: readline binds into whichever keymap is
# current otherwise, and in vi mode that changes under our feet. Typing keys
# live in emacs and vi-insert; Enter is also wrapped in vi-command.

__ficus_kms=(emacs vi-insert)

# What a key does now in keymap $1: "f name" (readline function),
# "m \"macro\"" or "x command" (bind -x). Empty when unbound or already
# ficus's (loading twice must not capture our own bindings as the user's).
declare -A __ficus_bindings
for __ficus_km in emacs vi-insert vi-command; do
  __ficus_bindings[$__ficus_km]=$(bind -m "$__ficus_km" -p 2>/dev/null | sed 's/^/f /'
    bind -m "$__ficus_km" -s 2>/dev/null | sed 's/^/m /'
    bind -m "$__ficus_km" -X 2>/dev/null | sed 's/^/x /')
done
__ficus_original() {  # keymap key; result in __ficus_o
  local km=$1 seq=$2 line kind rest
  __ficus_o=''
  while IFS= read -r line; do
    kind=${line%% *} rest=${line#* }
    [[ $rest == "\"$seq\""[:\ ]* ]] || continue
    rest=${rest#"\"$seq\""}
    rest=${rest#:}
    rest=${rest# }
    if [[ $kind == x ]]; then
      # bind -X quotes the command; undo \" and \\ only (no eval: $ stays).
      rest=${rest#\"}
      rest=${rest%\"}
      rest=${rest//\\\\/$'\001'}
      rest=${rest//\\\"/\"}
      rest=${rest//$'\001'/\\}
    fi
    [[ $rest == *__ficus* || $rest == *'\C-x\C-]'* ]] && return
    __ficus_o="$kind $rest"
    return
  done <<<"${__ficus_bindings[$km]}"
}

# Bind key in keymap to a saved action ("f name" | "m macro" | "x command" | "").
__ficus_bind_action() {
  local km=$1 key=$2 action=$3
  case ${action%% *} in
    f|m) bind -m "$km" "\"$key\": ${action#* }" ;;
    x) bind -m "$km" -x "\"$key\": ${action#* }" ;;
    *) bind -m "$km" -r "$key" ;;
  esac
}

# Original actions, per "keymap|key".
declare -A __ficus_orig
__ficus_save() {
  local km=$1 key=$2
  __ficus_original "$km" "$key"
  [[ -n $__ficus_o || -z ${__ficus_orig[$km|$key]+set} ]] && __ficus_orig[$km|$key]=$__ficus_o
}

# Keys that act on an open menu, by step id. While the menu is open each is a
# two-step macro: step 1 checks the menu still applies, then either acts and
# makes step 2 a no-op, or closes the menu and makes step 2 the key's
# original action. With the menu closed the keys are bound to their original
# actions directly, so readline sees the real function (repeated Tab lists).
# Configured keys get one id per sequence (prefix + index): no id may be a
# prefix of another, or readline would wait for more input.
declare -A __ficus_menu_key=( [u]='\e[A' [U]='\eOA' [d]='\e[B' [D]='\eOB' )
declare -A __ficus_menu_act=( [u]=__ficus_prev [U]=__ficus_prev [d]=__ficus_next [D]=__ficus_next )
__ficus_add_keys() {  # id-prefix function sequence...
  local prefix=$1 act=$2 i=0 seq
  shift 2
  for seq; do
    __ficus_menu_key[$prefix$i]=$seq __ficus_menu_act[$prefix$i]=$act
    i=$(( i + 1 ))
  done
}
__ficus_add_keys t __ficus_accept "${__ficus_key_accept[@]}"
__ficus_add_keys g __ficus_dismiss "${__ficus_key_dismiss[@]}"
__ficus_add_keys n __ficus_group_next "${__ficus_key_next_group[@]}"
__ficus_add_keys p __ficus_group_prev "${__ficus_key_prev_group[@]}"
for __ficus_km in "${__ficus_kms[@]}"; do
  for __ficus_id in "${!__ficus_menu_key[@]}"; do
    __ficus_save "$__ficus_km" "${__ficus_menu_key[$__ficus_id]}"
    bind -m "$__ficus_km" -x "\"\\C-x\\C-]1$__ficus_id\": __ficus_step $__ficus_id"
  done
done
__ficus_step() {
  local id=$1 km act=${__ficus_menu_act[$1]} ok=0
  if __ficus_menu_valid; then
    ok=1
    # Up belongs to history until Down picked an item (up_arrow = history).
    [[ $act == __ficus_prev && $__ficus_cfg_up != menu ]] && (( ! __ficus_picked )) && ok=0
  elif [[ $act == __ficus_accept && -n $READLINE_LINE ]] && (( __ficus_held || ! __ficus_cfg_auto )) && __ficus_at_end; then
    # The menu was held back (short command word) or auto_popup is off:
    # Tab opens it, and it then follows typing.
    __ficus_manual=1 __ficus_sel=0 __ficus_picked=0
    __ficus_query
    if (( __ficus_n )); then
      for km in "${__ficus_kms[@]}"; do bind -m "$km" -x "\"\\C-x\\C-]2$id\": :"; done
      __ficus_paint
      return
    fi
    __ficus_manual=0
  fi
  if (( ok )); then
    for km in "${__ficus_kms[@]}"; do bind -m "$km" -x "\"\\C-x\\C-]2$id\": :"; done
    "$act"
  else
    for km in "${__ficus_kms[@]}"; do
      __ficus_bind_action "$km" "\\C-x\\C-]2$id" "${__ficus_orig[$km|${__ficus_menu_key[$id]}]}"
    done
    __ficus_close
  fi
}
__ficus_bind_menu_keys() {
  [[ $__ficus_keys == menu ]] && return
  local km id
  for km in "${__ficus_kms[@]}"; do
    for id in "${!__ficus_menu_key[@]}"; do
      bind -m "$km" "\"${__ficus_menu_key[$id]}\": \"\\C-x\\C-]1$id\\C-x\\C-]2$id\""
    done
  done
  __ficus_keys=menu
}
# Only the accept key (Tab) acts: it opens a held menu.
__ficus_bind_accept_only() {
  [[ $__ficus_keys == accept ]] && return
  __ficus_bind_plain_keys
  local km id
  for km in "${__ficus_kms[@]}"; do
    for id in "${!__ficus_menu_key[@]}"; do
      [[ ${__ficus_menu_act[$id]} == __ficus_accept ]] || continue
      bind -m "$km" "\"${__ficus_menu_key[$id]}\": \"\\C-x\\C-]1$id\\C-x\\C-]2$id\""
    done
  done
  __ficus_keys=accept
}
__ficus_bind_plain_keys() {
  [[ $__ficus_keys == plain ]] && return
  local km id
  for km in "${__ficus_kms[@]}"; do
    for id in "${!__ficus_menu_key[@]}"; do
      __ficus_bind_action "$km" "${__ficus_menu_key[$id]}" "${__ficus_orig[$km|${__ficus_menu_key[$id]}]}"
    done
  done
  __ficus_keys=plain
}

# Readline re-binds the tty erase/kill/werase characters on every line unless
# told not to, which would silently drop the Backspace binding.
bind 'set bind-tty-special-chars off'
for __ficus_km in "${__ficus_kms[@]}"; do
  __ficus_save "$__ficus_km" '\C-?'
  __ficus_save "$__ficus_km" '\C-h'
done
__ficus_arm() {
  local km
  for km in "${__ficus_kms[@]}"; do
    bind -m "$km" -x '"\C-?": __ficus_backspace'
    bind -m "$km" -x '"\C-h": __ficus_backspace'
  done
  __ficus_armed=1
}
__ficus_disarm() {
  local km
  for km in "${__ficus_kms[@]}"; do
    __ficus_bind_action "$km" '\C-?' "${__ficus_orig[$km|\\C-?]:-f backward-delete-char}"
    __ficus_bind_action "$km" '\C-h' "${__ficus_orig[$km|\\C-h]:-f backward-delete-char}"
  done
  __ficus_armed=0
}

# Printable ASCII. Multibyte input keeps readline's own self-insert.
__ficus_bind_printable() {
  local i o c km
  for km in "${__ficus_kms[@]}"; do
    for (( i = 32; i < 127; i++ )); do
      printf -v o '%03o' "$i"
      printf -v c "\\$o"
      case $c in
        \') bind -m "$km" -x "\"\\$o\": __ficus_insert \"'\"" ;;
        *) bind -m "$km" -x "\"\\$o\": __ficus_insert $(printf '%q' "$c")" ;;
      esac
    done
  done
}
__ficus_bind_printable

# Keys that leave the edit position (or the line): run a ficus step first,
# then what the key did before. Steps: c close, e Enter, s search.
for __ficus_km in emacs vi-insert vi-command; do
  bind -m "$__ficus_km" -x '"\C-x\C-]c": __ficus_close'
  bind -m "$__ficus_km" -x '"\C-x\C-]e": __ficus_enter'
  bind -m "$__ficus_km" -x '"\C-x\C-]s": __ficus_search'
done
__ficus_wrap() {  # keymap key id [step]
  local km=$1 key=$2 id=$3 step=${4:-c}
  __ficus_save "$km" "$key"
  [[ -n ${__ficus_orig[$km|$key]} ]] || return 0
  __ficus_bind_action "$km" "\\C-x\\C-]k$id" "${__ficus_orig[$km|$key]}"
  bind -m "$km" "\"$key\": \"\\C-x\\C-]$step\\C-x\\C-]k$id\""
}
for __ficus_km in "${__ficus_kms[@]}"; do
  __ficus_wrap "$__ficus_km" '\C-m' m e
  __ficus_wrap "$__ficus_km" '\C-j' j e
  __ficus_wrap "$__ficus_km" '\C-r' S s
  __ficus_wrap "$__ficus_km" '\C-s' T s
  __ficus_wrap "$__ficus_km" '\e[C' r
  __ficus_wrap "$__ficus_km" '\e[D' l
  __ficus_wrap "$__ficus_km" '\eOC' R
  __ficus_wrap "$__ficus_km" '\eOD' L
  __ficus_wrap "$__ficus_km" '\e[H' h
  __ficus_wrap "$__ficus_km" '\e[F' E
  __ficus_wrap "$__ficus_km" '\C-a' a
  __ficus_wrap "$__ficus_km" '\C-e' z
  __ficus_wrap "$__ficus_km" '\C-b' b
  __ficus_wrap "$__ficus_km" '\C-f' f
  __ficus_wrap "$__ficus_km" '\C-u' u
  __ficus_wrap "$__ficus_km" '\C-w' w
  __ficus_wrap "$__ficus_km" '\C-k' K
  __ficus_wrap "$__ficus_km" '\eb' B
  __ficus_wrap "$__ficus_km" '\ef' F
  for __ficus_k in "${__ficus_key_pick[@]}"; do
    bind -m "$__ficus_km" -x "\"$__ficus_k\": __ficus_pick"
  done
done
# vi command mode: Enter only closes the menu (no completion: the user left
# insert mode on purpose).
__ficus_wrap vi-command '\C-m' M
__ficus_wrap vi-command '\C-j' J

__ficus_keys=menu
__ficus_bind_plain_keys
__ficus_arm
unset __ficus_k __ficus_km __ficus_id

fi
