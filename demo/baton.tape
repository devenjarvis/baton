# Baton demo tape.
#
# Self-contained deterministic demo. Sets up a throwaway git repo in /tmp,
# walks through version + doctor + launches the TUI, opens the new-agent
# prompt overlay, cancels, and quits. No real claude invocation required.
#
# Prerequisites for recording:
#   - vhs >= v0.10.0 installed (brew install vhs)
#   - baton binary built at the repo root (go build -o baton .)
#
# Regenerate with:
#   vhs demo/baton.tape

Output demo/baton.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set Framerate 24
Set PlaybackSpeed 1.0
Set TypingSpeed 40ms

# Set up a throwaway demo repo and put the freshly built baton on PATH.
Hide
Type "export PATH=$PWD:$PATH"
Enter
Type "rm -rf /tmp/baton-demo && mkdir -p /tmp/baton-demo && cd /tmp/baton-demo"
Enter
Type "git init -q && git config user.email demo@demo && git config user.name Demo"
Enter
Type `echo hello > README.md && git add . && git commit -qm init`
Enter
Type "clear"
Enter
Show

Sleep 500ms

# Show version.
Type "baton --version"
Enter
Sleep 1500ms

# Show environment checks.
Type "baton doctor"
Enter
Sleep 2500ms

Type "clear"
Enter
Sleep 300ms

# Launch the TUI dashboard.
Type "baton"
Enter
Sleep 2500ms

# Open the new-agent prompt overlay.
Type "n"
Sleep 1500ms

# Type a sample prompt to show the overlay in action.
Type "refactor internal/vt to reduce allocations"
Sleep 2000ms

# Cancel the prompt without spawning an agent.
Escape
Sleep 1500ms

# Quit the TUI (no running agents => immediate exit).
Type "q"
Sleep 1000ms

Hide
Type "cd - >/dev/null && rm -rf /tmp/baton-demo"
Enter
Show
