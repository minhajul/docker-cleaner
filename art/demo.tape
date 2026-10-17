# Records art/demo.gif and art/screenshot.png with VHS
# (https://github.com/charmbracelet/vhs). Run `make demo`; it builds
# internal/demo, which runs the TUI on canned data, not your Docker.

Output art/demo.gif

Set Shell "bash"
Set FontSize 15
Set Width 1480
Set Height 760
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 80ms

Hide
Type "./dist/demo" Enter
Sleep 1s
Show

Sleep 2s
Screenshot art/screenshot.png

# Browse, then sort by size.
Down 4
Sleep 600ms
Type "s"
Sleep 1.5s

# Only dangling images: select them and delete.
Type "D"
Sleep 1.5s
Type "a"
Sleep 1s
Type "d"
Sleep 2.5s
Type "y"
Sleep 2.5s

# Clear filters; show what nothing uses any more.
Escape
Sleep 1s
Type "U"
Sleep 2s
Escape
Sleep 800ms

# Search by name.
Type "/"
Sleep 400ms
Type "myapp"
Sleep 1s
Enter
Sleep 1.5s
Escape
Sleep 800ms

# Every key.
Type "?"
Sleep 2.5s
Type "?"
Sleep 500ms
Type "q"
Sleep 500ms
