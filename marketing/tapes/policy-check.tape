Output docs/assets/demos/pixlog-policy-check.gif
Output docs/assets/demos/pixlog-policy-check.mp4

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

Hide
Type "cd $PIXLOG_PROJECT_ROOT && bash scripts/marketing/prepare-recording.sh policy-check"
Enter
Wait+Screen /READY policy-check/
Type "cd dist/marketing/recordings/policy-check && clear && printf '\nPIXLOG // PROTECT IMAGE REGIONS IN CI\n\n'"
Enter
Show
Sleep 1.5s

Type "bash scripts/apply-unsafe-edit.sh"
Enter
Sleep 1.5s
Type "pixlog diff --staged -- assets/hero.png"
Enter
Sleep 5s
Type `pixlog check 2>&1 | sed "s|$PWD/||g"`
Enter
Sleep 5s
