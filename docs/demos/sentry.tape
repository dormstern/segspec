# VHS tape: Sentry self-hosted scan
# Pre-req: git clone --depth 1 https://github.com/getsentry/self-hosted /tmp/sentry
# Run: vhs docs/demos/sentry.tape

Output docs/demos/sentry-scan.gif

Set FontSize 15
Set Width 1000
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Padding 20

Type "# Sentry self-hosted: 70+ services."
Enter
Type "# How many network dependencies?"
Enter
Sleep 2s

Type "segspec analyze /tmp/sentry"
Enter
Sleep 4s

Sleep 3s

Type ""
Enter
Type "# 411 dependencies. Now with evidence:"
Enter
Sleep 1.5s

Type "segspec analyze /tmp/sentry --format evidence | head -40"
Enter
Sleep 5s

Sleep 3s
