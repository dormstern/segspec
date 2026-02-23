# VHS tape: Diff mode demo
# Pre-req: /tmp/baseline.json (from original state), /tmp/myapp (modified state)
# Run: vhs docs/demos/diff.tape

Output docs/demos/diff-demo.gif

Set FontSize 15
Set Width 1000
Set Height 600
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Padding 20

Type "# Someone added memcached and removed rabbitmq."
Enter
Type "# Does your CI catch that?"
Enter
Sleep 2.5s

Type "segspec diff /tmp/baseline.json /tmp/myapp --exit-code"
Enter
Sleep 4s

Sleep 3s
