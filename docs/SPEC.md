# SPEC — WhatHappened / `wh` v1 (binding)

**This spec is binding on every agent. The engine's debate verdicts override
draft plans, but they may NOT override this spec.**

---

## 1. Goal

A CLI (`wh`, Go) that captures **the last command and its output** from the
current terminal session and saves it as a reviewable directory of files.

It must work in **any terminal on any POSIX system** — including terminals with
no capture API at all (Alacritty, foot, GNOME Terminal, tmux, plain SSH) — and
use higher-fidelity capture where a terminal offers it (kitty).

Success looks like: in any terminal, press a keybinding or run `wh save`, and you
get a directory containing the exact command, its full output, and metadata.

## 2. Architecture (fixed — do not redesign)

Three layers, bottom-up:

```
L1 markers   shell hooks emit prompt boundaries (OSC 133 in-band + JSONL side-channel)
L2 providers capture bytes from a source (kitty CLI, tmux, PTY tap, heuristic)
L0 tap       `wh wrap <shell>` PTY proxy — universal foundation, no terminal support needed
L3 store     append-only session files → `wh save` slices a byte range → saved directory
```

Two rules that define the whole design:

1. **A terminal can give you bytes; only a shell can give you the command.**
   `capture.Capture.Command` is always populated from shell-integration metadata
   (`marker.Event.Cmd`), never scraped from screen text. A rendered prompt is
   unreliable (multi-line prompts, right-aligned prompts, prompt plugins).

2. **Record continuously, save on demand.** v1 has NO auto-save policies — saving
   is always a deliberate act. But the recorder must not lose output that has
   already scrolled off screen, which is why the tap records append-only.

## 3. Fixed contracts — do not modify, do not extend

These two packages were authored up front so parallel batches interoperate.
**Treat them as read-only.** If you believe a contract is wrong, do NOT edit it;
put the objection in `blockedOn`.

- `internal/capture/capture.go` — `Provider` interface, `Capture`, `Request`, `Scope`, `Fidelity`.
- `internal/marker/marker.go` — `Event`, `Type`, OSC 133 constants, `UnknownOffset`.

You MAY add new files to these packages **only if** they add new behaviour without
changing any existing exported identifier's meaning. If in doubt, use your own package.

## 4. Hard constraints — violating any of these fails the batch

- **Workspace path contains a `?`.** The repo lives at
  `/media/Storage/home-gfardad/Projects/WhatHappened?`.
  `?` is a shell glob character. **Always quote the path** in every bash command
  (`cd "/media/Storage/home-gfardad/Projects/WhatHappened?"`). Prefer using the
  `workdir` parameter of the bash tool instead of `cd` wherever possible.
  A batch that writes files to the wrong path has failed, regardless of code quality.
- **No new module dependencies.** `go.mod` is pre-populated with
  `github.com/creack/pty v1.1.24` and is Manager-owned. Use the **standard library
  only**, plus `creack/pty`. Do not run `go mod tidy` (parallel batches race on it).
  If you genuinely need another dependency, put it in `blockedOn` and do without.
- **No cross-batch imports.** You may import: the standard library, `creack/pty`,
  `whathappened/internal/capture`, `whathappened/internal/marker`.
  You may **NOT** import any other `whathappened/internal/...` package created by a
  different batch — those may not exist yet and your build will break. Wiring
  happens in a later integration batch.
- **Go 1.27.2, module `whathappened`.** No build tags gating POSIX code, no cgo.
- **Verify only your own paths.** `go build ./...` may fail because a parallel
  batch is mid-edit; that is not your defect. Verify with
  `go vet ./<your-paths>/...` and `go test ./<your-paths>/...` and report real output.
- **Web tools may be unavailable** (`mcp__websearch__*`, `mcp__context7__*`).
  Local ground truth wins: `kitten @ <cmd> --help`, `man 5 kitty.conf`, and the
  installed kitty docs at `/usr/share/doc/kitty/html/` are authoritative. Verified
  facts about kitty 0.49.2 are in `docs/DESIGN.md`; treat anything else as unverified
  and say so rather than guessing.

## 5. Out of scope for v1

- Windows support (no ConPTY, no Windows Terminal capture). POSIX only.
- WezTerm, Ghostty, iTerm2, Konsole, VS Code providers. (Later milestone. Only
  kitty, tmux, and the PTY tap are in scope.)
- Auto-save policies (failures-only, non-empty-only). Manual save only.
- Search index, retention/GC of old sessions, redaction engine beyond a simple
  documented pass-through hook.
- Any GUI, TUI, or keybinding installer. Emit keybinding snippets in docs only.

## 6. Acceptance (whole-project, checked by the integration batch)

- `go build ./...` and `go vet ./...` are clean.
- `go test ./...` passes with real tests (not empty stubs).
- `wh save` produces a directory containing `meta.json`, `output.ansi`,
  `output.txt`, `README.md`.
- Saved files are created with `0600` inside a `0700` directory (terminal output
  contains secrets — this is a security requirement, not a nicety).
- `wh wrap bash` works in a non-interactive harness: a scripted shell runs a
  command, exits, and `wh save --session <id>` recovers that exact command and output.
- Every artefact is traceable: `meta.json` records which provider produced it,
  the fidelity, and any caveats.

## 7. Batch write scopes (disjoint — never write outside yours)

| Batch | Owns (exclusive) |
|---|---|
| 1 — store + render | `internal/store/`, `internal/render/` |
| 2 — providers | `internal/provider/` |
| 3 — pty tap | `internal/tap/` |
| 4 — shell integration | `shell/`, `internal/shellint/` |
| 5 — CLI + docs (integration, runs LAST) | `cmd/wh/main.go`, `README.md`, `Makefile`, `docs/` |
