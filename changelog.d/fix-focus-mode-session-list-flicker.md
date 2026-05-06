### Fixed

- Focus mode SESSIONS list no longer flickers/reorders while agents stream output. Same-priority sessions now sort by `Session.CreatedAt` instead of `Session.LastOutputTime()`, so the order only shifts when a session crosses a priority boundary (e.g. `active → waiting`, `idle → error`) — i.e. on a real state change.
