### Fixed

- Pasting into the fullscreen agent terminal launched from focus mode (space/enter on a session block) now works. The focus-launch panel was only forwarding `tea.KeyPressMsg` and silently dropping `tea.PasteMsg`, so clipboard content never reached the agent. Paste events are now forwarded via `Agent.Paste` just like the regular dashboard terminal preview.
