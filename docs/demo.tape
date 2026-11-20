# Microgravity (mg) Terminal Demo
# Render with: vhs docs/demo.tape

Output docs/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 760
Set Padding 20
Set Theme "TokyoNight"
Set TypingSpeed 40ms

# 1. Start local-only chat routed through Ollama
Type "mg --local --routing-mode local-only"
Enter
Sleep 2s

# Switch to Tokyo Night theme inside the TUI
Type "/theme tokyo-night"
Enter
Sleep 1s

# 2. Ask the local model to inspect and update a file (streams tokens + collapsible tool cards)
Type "Inspect README.md and update the quickstart note, then show the diff preview."
Enter
Sleep 4s

# Toggle collapsible tool card details
Tab
Sleep 1.5s
Tab
Sleep 1s

# 3. Approve inline diff preview modal ([a]pprove / [r]eject / [A]lways allow)
Type "a"
Sleep 2s

# Toggle live swarm/subagent monitor pane and checkpoint rewind check
Type "/swarm"
Enter
Sleep 1.5s
Type "/swarm"
Enter
Sleep 800ms

# Exit chat session cleanly
Type "/exit"
Enter
Sleep 1s

# 4. Launch live terminal telemetry dashboard
Type "mg dashboard --live"
Enter
Sleep 3s
Type "q"
Sleep 1s
