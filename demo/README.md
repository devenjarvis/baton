# Demo assets

This directory holds the [VHS](https://github.com/charmbracelet/vhs) tape used to produce `baton.gif` — the animated demo embedded in the top-level README.

## Regenerating the GIF

Requirements:

- VHS v0.10.0+ (`brew install vhs`)
- `baton` on PATH
- `claude` on PATH (real, or a deterministic stub for reproducible recordings)
- A prepared demo repo — see the prerequisite comments at the top of `baton.tape`

Then:

```bash
vhs demo/baton.tape
```

The output is written to `demo/baton.gif`. Commit the regenerated GIF alongside any tape changes.

## Keeping the GIF small

Target ≤ 2 MB so GitHub renders it inline on mobile. If the output is larger, lower the framerate or trim idle sleeps rather than reducing resolution — readability of the TUI text matters more than smoothness.
