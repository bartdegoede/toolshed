# Render with: just screenshots   (docs/import.png and docs/import-secrets.png)
Output "/tmp/toolshed-screenshots.gif"

Set Shell bash
Set FontSize 17
Set Width 1400
Set Height 760
Set Padding 24
Set Theme "Catppuccin Mocha"
Set CursorBlink false

Hide
Type "bash --norc --noprofile" Enter
Type "source demo/screenshots.sh && clear" Enter
Sleep 4s
Type "toolshed" Enter
Sleep 2s
Type "m" Sleep 1.5s
Down Sleep 500ms
Show
Sleep 500ms
Screenshot docs/import.png
Hide
Enter Sleep 1.5s
Show
Sleep 500ms
Screenshot docs/import-secrets.png
Hide
Escape Sleep 300ms Escape Sleep 300ms Type "q"
