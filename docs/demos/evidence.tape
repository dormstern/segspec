# VHS tape: Evidence demo (hero GIF for README)
# Pre-req: git clone --depth 1 https://github.com/PostHog/posthog /tmp/posthog
# Run: vhs docs/demos/evidence.tape

Output docs/demos/evidence-demo.gif

Set FontSize 15
Set Width 1000
Set Height 650
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Padding 20

Type "segspec analyze /tmp/posthog --format evidence"
Enter
Sleep 5s

Sleep 4s
