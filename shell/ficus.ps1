# ficus: as-you-type completion menu for PowerShell (PSReadLine).
# Load with:  Invoke-Expression (& ficus init pwsh | Out-String)   (last in $PROFILE)
#
# Printable keys and Backspace get PSReadLine handlers that edit through the
# public API (Insert, BackwardDeleteChar) and then paint the menu below the
# line with ANSI escapes. Every key ficus takes keeps the handler it had
# before (built-in function, the user's script block or another module's
# handler, read from PSReadLine's dispatch table) and runs it when the menu
# does not apply.

# The whole integration is one if-block: a `return` here would return from
# the user's profile that runs this. Hosts without PSReadLine, or consoles
# without escape sequence support (legacy conhost), skip silently.
if (((Get-Module PSReadLine) -or (Import-Module PSReadLine -PassThru -ErrorAction SilentlyContinue)) -and
    $Host.UI.SupportsVirtualTerminal) {

$__ficusRL = [Microsoft.PowerShell.PSConsoleReadLine]
$__ficusNonPublic = [Reflection.BindingFlags]'NonPublic,Instance'
if (-not $global:__ficus) {
    $global:__ficus = @{
        Orig = @{}     # chord -> Action<ConsoleKeyInfo?,object> it had before ficus
    }
}
$__ficus.Bin       = @FICUS@
@CONFIG@
$__ficus.Colors    = 256      # terminfo is not available; consoles with VT do 256
$__ficus.Held      = $false   # the engine hides a menu Tab would open
$__ficus.Manual    = $false   # Tab opened the menu: show it in full while typing
# A screen reader cannot follow a menu painted below the line: only Tab
# opens it, and it carries no colors.
$__ficusRLOption = Get-PSReadLineOption
if ($__ficusRLOption.PSObject.Properties['ScreenReaderModeEnabled'] -and $__ficusRLOption.ScreenReaderModeEnabled) {
    $__ficus.Auto = $false
    $__ficus.Colors = 0
}
# PSReadLine's own predictions draw in the same place as the ghost text.
$__ficus.GhostOn = $__ficus.Ghost -and (-not $__ficusRLOption.PSObject.Properties['PredictionSource'] -or
    "$($__ficusRLOption.PredictionSource)" -eq 'None')
$__ficus.GhostShown = $false
$__ficus.N         = 0        # matches in the open menu
$__ficus.Sel       = 0
$__ficus.Drawn     = 0        # menu rows on screen
$__ficus.Picked    = $false   # selection moved with Up/Down
$__ficus.Presel    = $false   # the engine highlights the best match: Enter takes it
$__ficus.TTL       = [TimeSpan]::FromSeconds(10)
$__ficus.NativeKey = $null    # query/directory/time of the cached native candidates
$__ficus.NativeTsv = $null
$__ficus.NativeAt  = [DateTime]::MinValue
# PSReadLine keeps the prompt's absolute row in a private field; when the
# menu scrolls the screen it must follow, or the next render lands one row off.
$__ficus.Singleton = $__ficusRL.GetField('_singleton', [Reflection.BindingFlags]'NonPublic,Static')
$__ficus.InitialY  = $__ficusRL.GetField('_initialY', $__ficusNonPublic)
$__ficus.Dispatch  = $__ficusRL.GetField('_dispatchTable', $__ficusNonPublic)

# Aliases, functions and cmdlets are invisible to PATH scanning. The file is
# private to this session (not inherited through the environment) and written
# without a byte order mark (Windows PowerShell 5.1's utf8 adds one).
if ($__ficus.Cmds) { Remove-Item -LiteralPath $__ficus.Cmds -ErrorAction SilentlyContinue }
$__ficus.Cmds = $null
try {
    $__ficus.Cmds = [IO.Path]::GetTempFileName()
    $lines = Get-Command -CommandType Alias, Function, Cmdlet -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -notmatch '[:\\/]' } |
        ForEach-Object {
            $desc = if ($_.CommandType -eq 'Alias') { "alias: $($_.Definition)" } else { "$($_.CommandType)".ToLower() }
            "$($_.Name)`t$desc"
        }
    [IO.File]::WriteAllLines($__ficus.Cmds, [string[]]$lines, [Text.UTF8Encoding]::new($false))
    if (-not $__ficus.ExitHooked) {
        Register-EngineEvent -SourceIdentifier PowerShell.Exiting -SupportEvent -Action {
            if ($global:__ficus.Cmds) { Remove-Item -LiteralPath $global:__ficus.Cmds -ErrorAction SilentlyContinue }
        } | Out-Null
        $__ficus.ExitHooked = $true
    }
} catch { }

# The file system directory ficus runs in: $PWD, or the last file system
# location when the current one is another provider (registry, env:, ...).
function global:__FicusCwd {
    $loc = Get-Location
    if ($loc.Provider.Name -eq 'FileSystem') { $__ficus.Cwd = $loc.ProviderPath }
    if ($__ficus.Cwd) { return $__ficus.Cwd }
    return (Get-Location -PSProvider FileSystem).ProviderPath
}

# PowerShell's own completions for arguments of cmdlets, functions and
# scripts, which carapace does not know. $null leaves it to the engine.
#
# TabExpansion2 is asked once per word: for the line up to the word's fetch
# prefix (a leading "-" for parameters, the directory part of a path), like
# the engine asks carapace. The engine fuzzy-filters the rest of the word.
# Results expire like the engine's cache, and after every command.
function global:__FicusNative([string]$LBuffer) {
    $ast = [System.Management.Automation.Language.Parser]::ParseInput($LBuffer, [ref]$null, [ref]$null)
    $cmd = $ast.FindAll({ $args[0] -is [System.Management.Automation.Language.CommandAst] }, $true) |
        Where-Object { $_.Extent.StartOffset -le $LBuffer.Length } | Select-Object -Last 1
    if (-not $cmd -or $cmd.CommandElements[0].Extent.EndOffset -ge $LBuffer.Length) { return $null }
    $name = $cmd.GetCommandName()
    if (-not $name) { return $null }
    $info = Get-Command $name -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($info.CommandType -eq 'Alias') { $info = $info.ResolvedCommand }
    if ($info.CommandType -notin 'Cmdlet', 'Function', 'Filter', 'ExternalScript') { return $null }

    $start = $LBuffer.LastIndexOfAny([char[]]" `t") + 1
    $word = $LBuffer.Substring($start)
    $keep = if ($word.StartsWith('-')) { 1 } else { $word.LastIndexOfAny([char[]]'/\') + 1 }
    $query = $LBuffer.Substring(0, $start + $keep)
    $key = "$query`0$PWD"
    if ($__ficus.NativeKey -eq $key -and ([DateTime]::UtcNow - $__ficus.NativeAt) -lt $__ficus.TTL) {
        return $__ficus.NativeTsv
    }

    $res = TabExpansion2 -inputScript $query -cursorColumn $query.Length
    $sep = [IO.Path]::DirectorySeparatorChar
    $tsv = ($res.CompletionMatches | ForEach-Object {
        $value = $_.CompletionText
        $tip = ''
        if ($_.ResultType -eq 'ProviderContainer' -and $value -notmatch "['`"]$") {
            $value += $sep   # continue into the directory (no trailing space)
        } elseif ($_.ResultType -ne 'ProviderItem') {
            # File tooltips are just the full path; keep the rest.
            $tip = ($_.ToolTip -replace '\s+', ' ').Trim()
            if ($tip -eq $_.ListItemText -or $tip -eq $_.CompletionText) { $tip = '' }
        }
        "{0}`t{1}`t{2}" -f ($value -replace '[\t\r\n]', ' '), ($_.ListItemText -replace '[\t\r\n]', ' '), $tip
    }) -join "`n"
    $__ficus.NativeKey = $key
    $__ficus.NativeTsv = $tsv
    $__ficus.NativeAt = [DateTime]::UtcNow
    return $tsv
}

# Run ficus with stdin, stdout and stderr all redirected. A child that
# inherits the console makes .NET return the terminal to cooked mode while it
# runs, and keys typed meanwhile go through the kernel line discipline (^U
# would kill the pending input). The line travels in the environment (Windows
# argument passing mangles embedded quotes); native candidates through stdin
# (environment blocks are limited to 32 KiB on Windows). Returns stdout lines.
function global:__FicusInvoke([string[]]$ArgList, [string]$LBuffer) {
    $psi = [Diagnostics.ProcessStartInfo]::new($__ficus.Bin)
    $native = __FicusNative $LBuffer
    if ($null -ne $native) { $ArgList += '--external' }
    $psi.Arguments = $ArgList -join ' '   # flags only: no quoting needed
    $psi.WorkingDirectory = __FicusCwd
    $psi.Environment['FICUS_LBUFFER'] = $LBuffer
    if ($__ficus.Cmds) { $psi.Environment['FICUS_EXTRA_COMMANDS'] = $__ficus.Cmds }
    $psi.UseShellExecute = $false
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = [Text.UTF8Encoding]::new($false)
    try {
        $p = [Diagnostics.Process]::Start($psi)
    } catch {
        return @()
    }
    $err = $p.StandardError.ReadToEndAsync()
    $out = $p.StandardOutput.ReadToEndAsync()
    if ($null -ne $native) {
        $in = [IO.StreamWriter]::new($p.StandardInput.BaseStream, [Text.UTF8Encoding]::new($false))
        $in.Write($native)
        $in.Close()
    } else {
        $p.StandardInput.Close()
    }
    $p.WaitForExit()
    [void]$err.Result
    $text = $out.Result
    if ($text.EndsWith("`n")) { $text = $text.Substring(0, $text.Length - 1) }
    if ($text -eq '') { return @() }
    return ($text -split "`r?`n")
}

function global:__FicusState {
    $line = $null; $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
    return $line, $cursor
}

# Paint lines below the cursor (which PSReadLine left at the end of the
# line), or erase the previous menu when lines is empty. When the previous
# frame has the same geometry only the rows that changed are rewritten, and
# every frame is a synchronized update (DEC mode 2026) so terminals that
# support it never show a half-drawn menu.
function global:__FicusPaint([string[]]$Lines, [string]$Ghost) {
    $x = [Console]::CursorLeft; $y = [Console]::CursorTop
    $esc = [char]27
    if (-not $Lines -or $Lines.Count -eq 0) {
        $__ficus.Shown = $null
        if ($__ficus.Drawn -eq 0) { return }
        [Console]::Write("$esc[?2026h`n`r$esc[J$esc[A$esc[?2026l")
        [Console]::SetCursorPosition($x, [Console]::CursorTop)
        $__ficus.Drawn = 0
        return
    }
    $sb = [Text.StringBuilder]::new()
    $width = [Console]::WindowWidth
    $old = $__ficus.Shown
    if ($old -and $old.Count -eq $Lines.Count -and $__ficus.Drawn -eq $Lines.Count -and
        $__ficus.ShownWidth -eq $width -and $__ficus.ShownY -eq $y) {
        [void]$sb.Append("$esc[?2026h")
        for ($i = 0; $i -lt $Lines.Count; $i++) {
            if ($Lines[$i] -ceq $old[$i]) { continue }
            [void]$sb.Append("$esc[$($i + 1)B`r$esc[2K$($Lines[$i])$esc[$($i + 1)A")
        }
        [void]$sb.Append("$esc[?2026l")
        [Console]::Write($sb.ToString())
        [Console]::SetCursorPosition($x, $y)
        $__ficus.Shown = $Lines
        __FicusGhost $Ghost $x $y
        return
    }
    # Newlines scroll when the menu does not fit; measure how far.
    [void]$sb.Append("$esc[?2026h")
    [void]$sb.Append("`n" * $Lines.Count)
    [Console]::Write($sb.ToString())
    $scrolled = ($y + $Lines.Count) - [Console]::CursorTop
    $top = $y - $scrolled
    [void]$sb.Clear()
    [void]$sb.Append("$esc[$($Lines.Count)A`r$esc[1B$esc[J")
    [void]$sb.Append(($Lines -join "`r`n"))
    [void]$sb.Append("$esc[?2026l")
    [Console]::Write($sb.ToString())
    [Console]::SetCursorPosition($x, $top)
    if ($scrolled -gt 0 -and $__ficus.InitialY) {
        $rl = $__ficus.Singleton.GetValue($null)
        $__ficus.InitialY.SetValue($rl, $__ficus.InitialY.GetValue($rl) - $scrolled)
    }
    $__ficus.Drawn = $Lines.Count
    $__ficus.Shown = $Lines
    $__ficus.ShownWidth = $width
    $__ficus.ShownY = $top
    __FicusGhost $Ghost $x $top
}

# Draw the ghost text (already styled by the engine) after the cursor at
# ($x, $y), the end of the line, unless it would wrap into the menu.
function global:__FicusGhost([string]$Ghost, [int]$x, [int]$y) {
    if (-not $Ghost) { return }
    $plain = $Ghost -replace "$([char]27)\[[0-9;]*m", ''
    if ($x + $plain.Length -ge [Console]::WindowWidth) { return }
    [Console]::Write("$([char]27)[K$Ghost")
    [Console]::SetCursorPosition($x, $y)
    $__ficus.GhostShown = $true
}

# Erase the ghost text: everything right of the cursor, which only holds the
# ghost while the cursor is at the end of the line.
function global:__FicusClearGhost {
    if (-not $__ficus.GhostShown) { return }
    $__ficus.GhostShown = $false
    $line, $cursor = __FicusState
    if ($cursor -eq $line.Length) { [Console]::Write("$([char]27)[K") }
}

function global:__FicusClose {
    $__ficus.N = 0; $__ficus.Sel = 0; $__ficus.Picked = $false; $__ficus.Held = $false; $__ficus.Presel = $false
    __FicusClearGhost
    __FicusPaint @()
}

function global:__FicusRefresh {
    $line, $cursor = __FicusState
    if (-not $line) { $__ficus.Manual = $false }
    if (-not $line -or $cursor -ne $line.Length) { __FicusClose; return }
    $width = [Console]::WindowWidth
    # Nothing is highlighted until Up/Down chooses an item.
    $flags = @('--sel', $__ficus.Sel, '--height', [Console]::WindowHeight, '--colors', $__ficus.Colors, '--theme', $__ficus.Theme, '--budget', $__ficus.Budget)
    if (-not $__ficus.Picked) { $flags += '--unselected' }
    if ($__ficus.Manual) { $flags += '--manual' }
    if ($__ficus.Move) { $flags += @('--move', $__ficus.Move) }
    if ($__ficus.GhostOn) { $flags += '--ghost' }
    $out = @(__FicusInvoke (@('complete', '--shell', 'pwsh', '--width', $width) + $flags) $line)
    if ($out.Count -eq 0) { __FicusClose; return }
    # Header: "n sel held presel<TAB>ghost".
    $head, $ghost = $out[0] -split "`t", 2
    $n, $sel, $held, $presel = $head -split ' '
    $__ficus.N = [int]$n; $__ficus.Sel = [int]$sel; $__ficus.Held = $held -eq '1'; $__ficus.Presel = $presel -eq '1'
    __FicusClearGhost
    __FicusPaint @($out | Select-Object -Skip 1) $ghost
}

function global:__FicusMenuOpen {
    if ($__ficus.N -eq 0) { return $false }
    $line, $cursor = __FicusState
    return $cursor -eq $line.Length
}

# Run what the chord did before ficus took it (any kind of handler). Enter
# must always run the line, even if its original could not be read.
function global:__FicusFallthrough([string]$Chord, $key, $arg) {
    $action = $__ficus.Orig[$Chord]
    if ($action) { $action.Invoke($key, $arg) }
    elseif ($Chord -eq 'Enter') { [Microsoft.PowerShell.PSConsoleReadLine]::AcceptLine($key, $arg) }
}

# --- key handlers -----------------------------------------------------------

# The handlers bound before ficus, read from PSReadLine's dispatch table by
# chord name (the key's ToString() is the chord string ficus binds, on 2.0.0
# as on 2.4; Get-PSReadLineKeyHandler -Chord does not exist before 2.0.1).
# Ordinal keys: "a" and "A" are different chords.
$__ficusBefore = [Collections.Generic.Dictionary[string, object]]::new([StringComparer]::Ordinal)
if ($__ficus.Dispatch) {
    foreach ($entry in $__ficus.Dispatch.GetValue($__ficus.Singleton.GetValue($null)).GetEnumerator()) {
        $__ficusBefore[$entry.Key.ToString()] = $entry.Value
    }
}
if (-not ($__ficus.Orig -is [Collections.Generic.Dictionary[string, object]])) {
    $__ficus.Orig = [Collections.Generic.Dictionary[string, object]]::new([StringComparer]::Ordinal)
}

# Remember the handler a chord has now (unless it is already ficus's, when
# loaded twice, or a plain SelfInsert ficus does itself), then give the chord
# to ficus. The handler gets the chord name baked in, so the fallthrough
# never has to guess it from the key.
function global:__FicusBind([string]$Chord, [string]$Body, [string]$Description) {
    $prev = $null
    if ($__ficusBefore.TryGetValue($Chord, [ref]$prev) -and $prev.LongDescription -notlike 'ficus:*') {
        [void]$__ficus.Orig.Remove($Chord)
        if ($prev.Action -and $prev.Action.Method.Name -ne 'SelfInsert') { $__ficus.Orig[$Chord] = $prev.Action }
    }
    $literal = $Chord -replace "'", "''"
    $sb = [scriptblock]::Create("param(`$key, `$arg)`n`$chord = '$literal'`n$Body")
    Set-PSReadLineKeyHandler -Chord $Chord -ScriptBlock $sb -Description "ficus: $Description"
}

# Printable keys and Space insert (or run a custom handler the user had on
# them, e.g. smart quotes) and refresh the menu.
$__ficusInsertBody = @'
    if ($__ficus.Orig[$chord]) { __FicusFallthrough $chord $key $arg }
    else { [Microsoft.PowerShell.PSConsoleReadLine]::Insert([string]$key.KeyChar) }
    $__ficus.Sel = 0; $__ficus.Picked = $false
    # More keys already waiting (a paste, fast typing): the last one
    # refreshes. With auto_popup off only a menu Tab opened follows typing.
    if ([Console]::KeyAvailable -or -not ($__ficus.Auto -or $__ficus.Manual)) { __FicusClose } else { __FicusRefresh }
'@
foreach ($i in 33..126) { __FicusBind ([string][char]$i) $__ficusInsertBody 'insert' }
__FicusBind 'Spacebar' $__ficusInsertBody 'insert'

__FicusBind 'Backspace' @'
    if ($__ficus.Orig[$chord]) { __FicusFallthrough $chord $key $arg }
    else { [Microsoft.PowerShell.PSConsoleReadLine]::BackwardDeleteChar($key, $arg) }
    $__ficus.Sel = 0; $__ficus.Picked = $false
    if ([Console]::KeyAvailable -or -not ($__ficus.Auto -or $__ficus.Manual)) { __FicusClose } else { __FicusRefresh }
'@ 'backspace'

# With the menu held back (a short command word) or auto_popup off, the
# accept key at the end of the line opens it; it then follows typing.
$__ficusAcceptBody = @'
    $line, $cursor = __FicusState
    if (-not (__FicusMenuOpen)) {
        if (($__ficus.Held -or -not $__ficus.Auto) -and $line -and $cursor -eq $line.Length) {
            $__ficus.Manual = $true; $__ficus.Sel = 0; $__ficus.Picked = $false
            __FicusRefresh
            if ($__ficus.N -gt 0) { return }
            $__ficus.Manual = $false
        }
        __FicusClose; __FicusFallthrough $chord $key $arg; return
    }
    # With nothing picked, Tab first inserts what all matches share.
    $flags = @('accept', '--shell', 'pwsh', '--sel', $__ficus.Sel)
    if (-not $__ficus.Picked) { $flags += '--unselected' }
    $new = (@(__FicusInvoke $flags $line.Substring(0, $cursor)) -join "`n")
    if (-not $new) { return }
    __FicusClearGhost
    [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $cursor, $new)
    $__ficus.Sel = 0; $__ficus.Picked = $false
    __FicusRefresh
'@
foreach ($chord in $__ficus.KeyAccept) { __FicusBind $chord $__ficusAcceptBody 'accept' }

# The first Down chooses the first item, the first Up the last one. Up
# belongs to history until Down picked an item (up_arrow = history).
foreach ($pair in @(@('DownArrow', 1, 0, '$false'), @('UpArrow', -1, -1, "(`$__ficus.Up -ne 'menu')"))) {
    __FicusBind $pair[0] @"
    if (-not (__FicusMenuOpen) -or (-not `$__ficus.Picked -and $($pair[3]))) { __FicusClose; __FicusFallthrough `$chord `$key `$arg; return }
    if (`$__ficus.Picked -or `$__ficus.Presel) { `$__ficus.Sel += $($pair[1]) } else { `$__ficus.Sel = $($pair[2]) }
    `$__ficus.Picked = `$true
    __FicusRefresh
"@ 'select'
}

# Jump to the first item of the next/previous group (the engine knows them).
foreach ($pair in @(@($__ficus.KeyNext_group, 'next-group'), @($__ficus.KeyPrev_group, 'prev-group'))) {
    foreach ($chord in $pair[0]) {
        __FicusBind $chord @"
    if (-not (__FicusMenuOpen)) { __FicusClose; __FicusFallthrough `$chord `$key `$arg; return }
    `$__ficus.Move = '$($pair[1])'
    try { __FicusRefresh } finally { `$__ficus.Move = `$null }
    `$__ficus.Picked = `$true
"@ 'group'
    }
}

$__ficusDismissBody = @'
    if ($__ficus.Drawn -gt 0) { __FicusClose; return }
    __FicusFallthrough $chord $key $arg
'@
foreach ($chord in $__ficus.KeyDismiss) { __FicusBind $chord $__ficusDismissBody 'dismiss' }
# In Emacs mode Escape is the prefix of Alt-chords (ChordFirstKey): leave it.
if ((Get-PSReadLineOption).EditMode -eq 'Windows') {
    __FicusBind 'Escape' $__ficusDismissBody 'dismiss'
}

# Enter with an item highlighted (the first one, or one chosen with
# Up/Down) completes with it and closes the menu: it stays closed until the
# next typed key, so a second Enter runs the line. With no menu (or nothing
# highlighted, preselect off) it runs the line exactly as typed.
__FicusBind 'Enter' @'
    if (($__ficus.Picked -or $__ficus.Presel) -and (__FicusMenuOpen)) {
        $line, $cursor = __FicusState
        $new = (@(__FicusInvoke @('accept', '--shell', 'pwsh', '--sel', $__ficus.Sel) $line) -join "`n")
        if ($new) {
            __FicusClearGhost
            [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $cursor, $new)
            __FicusClose
            return
        }
    }
    __FicusClose
    $__ficus.Manual = $false
    $__ficus.NativeKey = $null   # the command may change what completes
    __FicusFallthrough $chord $key $arg
'@ 'run'

# RightArrow at the end of the line takes the ghost suggestion: the item it
# shows, in full. Elsewhere it moves, closing the menu.
__FicusBind 'RightArrow' @'
    if ($__ficus.GhostShown -and (__FicusMenuOpen)) {
        $line, $cursor = __FicusState
        $new = (@(__FicusInvoke @('accept', '--shell', 'pwsh', '--sel', $__ficus.Sel) $line) -join "`n")
        if ($new) {
            __FicusClearGhost
            [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $cursor, $new)
            $__ficus.Sel = 0; $__ficus.Picked = $false
            __FicusRefresh
            return
        }
    }
    __FicusClose
    __FicusFallthrough $chord $key $arg
'@ 'ghost'

# Keys that leave the edit position or the line: erase the menu first.
foreach ($chord in 'Ctrl+c', 'LeftArrow', 'Home', 'End', 'Ctrl+a', 'Ctrl+e', 'Ctrl+u', 'Ctrl+w') {
    __FicusBind $chord @'
    __FicusClose
    __FicusFallthrough $chord $key $arg
'@ 'close'
}

# Full-screen fuzzy picker over all candidates (in-process fzf) on
# Ctrl+Space (Unix terminals send it as NUL, which PSReadLine names Ctrl+@;
# ficus init lists both chords there). Unlike __FicusInvoke, fzf must own the console,
# so this is a plain native call (pwsh hands the terminal over and takes it
# back); $LASTEXITCODE is kept so prompts showing it stay right.
$__ficusPickBody = @'
    __FicusClose
    $line, $cursor = __FicusState
    $lbuffer = $line.Substring(0, $cursor)
    $argList = @('pick', '--shell', 'pwsh', '--fullscreen')
    $native = __FicusNative $lbuffer
    if ($null -ne $native) { $argList += '--external' }
    $saved = @{ Exit = $global:LASTEXITCODE; Enc = [Console]::OutputEncoding }
    $env:FICUS_LBUFFER = $lbuffer
    $env:FICUS_EXTRA_COMMANDS = $__ficus.Cmds
    $new = ''
    Push-Location -LiteralPath (__FicusCwd)
    try {
        [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
        $new = if ($null -ne $native) { (@($native | & $__ficus.Bin @argList 2>$null) -join "`n") }
               else { (@(& $__ficus.Bin @argList 2>$null) -join "`n") }
    } catch {
    } finally {
        Pop-Location
        [Console]::OutputEncoding = $saved.Enc
        Remove-Item Env:FICUS_LBUFFER, Env:FICUS_EXTRA_COMMANDS -ErrorAction SilentlyContinue
        $global:LASTEXITCODE = $saved.Exit
    }
    [Microsoft.PowerShell.PSConsoleReadLine]::InvokePrompt()
    if ($new) { [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $cursor, $new) }
'@
foreach ($chord in $__ficus.KeyPick) { __FicusBind $chord $__ficusPickBody 'pick' }

}
