# Records the README's demo GIF from the bundled sample data.
# Run `make gif` from the repository root; it builds bin/cronen first.

Output doc/demo.gif

Require ./bin/cronen

Set Shell bash
Set FontSize 16
Set Width 1280
Set Height 760
Set Padding 16
Set TypingSpeed 60ms
Set Theme "Catppuccin Mocha"

Hide
Type "clear" Enter
Show

Type "cronen --demo" Sleep 400ms
Hide
Ctrl+U
Type "./bin/cronen --demo" Enter
Sleep 1500ms
Show

# The flagged updates, one by one.
Sleep 2s
Down Sleep 900ms
Down Sleep 900ms
Down Sleep 900ms
Down Sleep 1200ms

# Scroll the detail pane.
Tab Sleep 600ms
Ctrl+D Sleep 1s
Tab Sleep 600ms

# Filter the list.
Type "/" Sleep 300ms
Type "nvidia" Sleep 1200ms
Escape Sleep 800ms

# The news, rendered.
Type "n" Sleep 2s
Tab Sleep 400ms
Ctrl+D Sleep 1500ms
Tab Sleep 400ms
Down Sleep 1500ms
Escape Sleep 800ms

# Help, then the upgrade confirmation (cancelled).
Type "?" Sleep 2s
Type "?" Sleep 600ms
Type "U" Sleep 2500ms
Escape Sleep 800ms

Type "q"
Sleep 500ms
