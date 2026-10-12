# ficus demo for README.md. Regenerate (needs vhs: brew install vhs):
#   make build && docs/demo-setup.sh "$PWD/bin/ficus" && vhs docs/demo.tape
Output docs/demo.gif

Set Shell "zsh"
Set FontSize 20
Set Width 1200
Set Height 640
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 110ms
Env LANG "en_US.UTF-8"

# vhs starts zsh without startup files: load the demo's own (ficus only).
Hide
Type "source /tmp/ficus-demo/zdot/.zshrc && cd /tmp/ficus-demo/project && clear"
Enter
Sleep 1s
Show

# Subcommands with descriptions while typing; the first item is highlighted.
Type "git ch"
Sleep 1.8s
Down
Sleep 1s
# Tab completes and opens the next word's menu: branches and tags, grouped.
Tab
Sleep 2.2s
Type "fe"
Sleep 1.2s
# Enter completes with the highlighted item; a second Enter runs the line.
Enter
Sleep 1s
Enter
Sleep 1.8s

# Flags with descriptions.
Type "ls -"
Sleep 1.8s
Type "l"
Sleep 1s
Enter
Sleep 800ms
Enter
Sleep 2s

# Ctrl-Space: fuzzy picker over every candidate, with a file preview.
Type "cat "
Sleep 1s
Ctrl+Space
Sleep 1.5s
Type "rea"
Sleep 1.5s
Enter
Sleep 900ms
Enter
Sleep 2.5s
