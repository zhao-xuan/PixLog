Output docs/assets/demos/pixlog-visual-blame.gif
Output docs/assets/demos/pixlog-visual-blame.mp4

Require bash
Require git
Require pixlog
Require chafa
Require magick

Set Shell "bash"
Set Width 1200
Set Height 720
Set FontSize 20
Set LineHeight 1.2
Set TypingSpeed 18ms
Set Framerate 30
Set Padding 22
Set Margin 24
Set MarginFill "#F4F1EA"
Set BorderRadius 8
Set WindowBar RingsRight
Set CursorBlink false
Set Theme { "name": "PixLog", "black": "#1C232A", "red": "#EE6B56", "green": "#55A36C", "yellow": "#F3C969", "blue": "#75BCD2", "magenta": "#A77A9B", "cyan": "#258B88", "white": "#FFFDF8", "brightBlack": "#68757D", "brightRed": "#FF8069", "brightGreen": "#70BC83", "brightYellow": "#FFD77E", "brightBlue": "#91D0E2", "brightMagenta": "#BF94B2", "brightCyan": "#45AAA7", "brightWhite": "#FFFFFF", "background": "#1C232A", "foreground": "#FFFDF8", "selection": "#3A4A53", "cursor": "#EE6B56" }

Env PS1 "pixlog-demo > "
Env GIT_PAGER "cat"

Hide
Type "cd $PIXLOG_PROJECT_ROOT && bash scripts/marketing/prepare-recording.sh visual-blame"
Enter
Wait+Screen /READY visual-blame/
Type "cd dist/marketing/recordings/visual-blame && clear && printf '\nPIXLOG // WHO CHANGED THIS PIXEL?\n\n'"
Enter
Show
Sleep 1.5s

Type "git log --oneline -3 -- assets/hero.png"
Enter
Sleep 2.5s
Type "pixlog diff HEAD~1 HEAD -- assets/hero.png"
Enter
Sleep 5s
Type "pixlog blame --point 760,145 assets/hero.png"
Enter
Sleep 5s
