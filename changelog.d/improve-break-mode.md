### Changed

- **Break mode is more immersive.** The breathing animation is now larger
  (27×9 vs the old 9×3), longer (a 36-frame, ~3.6 s cycle that lands near a
  guided-breathwork pace), smoother (cosine-eased radius, sparkle ring at
  peak), and shifts colour per breath cycle so the eye has something to
  settle on. Smaller terminals fall back to the original compact frames.
- **Break mode no longer drops you back into focus on its own.** When the
  break duration elapses, the screen flips to a warm bordered "BREAK
  COMPLETE" panel with a pulsing colour, an over-time counter, and a chime.
  Pressing `b` is the only way back to work — walk away as long as you want.
- **Break timer counts wall-clock time, not just monotonic ticks.** The
  start instant has its monotonic reading stripped, so suspending the
  laptop during a break still drains the timer. Coming back from lunch no
  longer leaves you with ten phantom minutes.
