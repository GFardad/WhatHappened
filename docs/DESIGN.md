# WhatHappened — design sketch

> Capture **the last command and its output** anywhere, save it as a reviewable artifact.
> Status: sketch. Nothing implemented yet.

---

## 1. The question this must answer first

> Terminal layer (kitty, ghostty, wezterm…) or shell layer (bash, zsh, fish…)?

**Answer: neither alone — they're two different problems, and you need both layers.**

| | Problem it solves | Can it do it alone? |
|---|---|---|
| **Shell layer** | **P1 — slicing.** Where does "the last command" *start and end*? | No. A shell knows the command line, cwd, exit code, and timing. It cannot see its own rendered output. |
| **Terminal layer** | **P2 — text.** What bytes were actually rendered? | **No.** It has the pixels but no command identity. Verified on kitty 0.49.2: `--extent last_cmd_output` gives *output only* — it cannot give you the command string that produced it. |
| **PTY tap** | **P2, universally.** | Yes, for POSIX. Doesn't ask the terminal for permission. |

Critically: **half the terminals you care about have no capture API at all**
(Alacritty, foot, GNOME Terminal, Windows Terminal, VS Code, Konsole-public-API).
If you build on terminal APIs, your "universal" tool has a hole exactly where
the most common terminals are. That alone decides it.

### The finding that reframes the project

Your current workaround:

```bash
kitten @ get-text | sed '$d'
```

**kitty already implements this properly.** From `kitten @ get-text --help` on 0.49.2:

```
--extent [=screen]
  last_cmd_output   the output of the last command that was run in the window
  first_cmd_output_on_screen
  last_non_empty_output
  ...
  The last four require shell_integration to be enabled.
```

And `shell_integration` defaults to `enabled` (`kitty.conf:2320` → `# shell_integration enabled`),
with integration scripts shipped for `bash`, `fish`, `ssh`, `zsh`.

So `sed '$d'` is a guess — "assume the last line is the next prompt" — where kitty has
*real prompt markers* and doesn't guess. The entire general version of this project is:
**give that capability to terminals that don't have it.** That is a much better
product framing than "a smarter sed".

---

## 2. The architecture

Three layers. The bottom two are universal; the top one is optional polish.

```
┌─ L3  Store ──────────────────────────────────────────────┐
│  session dir: events.jsonl + stream.ansi (append-only)   │
│  `wh save` slices a byte range → a saved directory        │
└───────────────────────────────────────────────────────────┘
┌─ L2  Providers (pluggable, ranked by fidelity) ──────────┐
│  kitty · tmux · zellij · wezterm · ghostty · iterm2 …     │
│  └── screen / heuristic  (last-resort, low fidelity)      │
└───────────────────────────────────────────────────────────┘
┌─ L1  Markers (prompt boundaries) ─────────────────────────┐
│  bash · zsh · fish · nu · pwsh                            │
│  emits OSC 133 in-band  AND  a side-channel JSONL         │
└───────────────────────────────────────────────────────────┘
        ▲
        └── L0  PTY tap (`wh wrap <shell>`) — the foundation.
            Sits between terminal and shell. Works with ANY
            terminal, over SSH, with no terminal cooperation.
```

### L1 emits markers **two ways, from one hook**

This is the key decision that makes the tool universal:

- **In-band** — `printf '\e]133;A\a'` … `B` (command) … `C` (output start) … `D;exit\a`.
  Parsed *by the emulator*, so kitty/wezterm/ghostty give you native
  `--extent last_cmd_output` for free.
- **Side-channel** — append a line to `$XDG_STATE_HOME/wh/sessions/<sid>/events.jsonl`:

  ```json
  {"seq":41,"t":"command_done","cmd":"cargo build --release","cwd":"~/src/foo","exit":0,"ts":1789…,"byte_end":184213}
  ```

  This never has to reach the emulator, so it works in terminals that **strip or
  ignore OSC 133 entirely** (Windows Terminal, VS Code, ConEmu, older VTE).
  One hook, two outputs, no fork of the integration script per terminal.

### L0 — the PTY tap is the real foundation

`wh wrap zsh` opens a pty, spawns the shell on the far side, and logs both
directions to `stream.ansi`, tagging byte offsets against L1 markers.

This buys something **no terminal API can give you: exact byte-range slicing.**
When `command_done` fires at byte offset `184213` and `command_start` was at
`181004`, the answer is `dd`-accurate. A terminal can only hand you a *rendered
view* and hope you can find the boundary in it.

It also makes the top two tiers genuinely universal: `alacritty | wh wrap zsh`
gives you real command/output separation with **zero terminal support**.

### L2 — providers are a small, honest interface

Three methods, no more. Keep them tiny and independent so one can fail without
touching the recorder.

```go
type Provider interface {
    Name() string
    Available() bool            // can we reach a terminal right now?
    Capture(ctx context.Context, req Request) (Capture, error)
}

type Capture struct {
    Raw      []byte    // faithful stream (ANSI preserved)
    Command  string    // from L1 metadata — terminals can't supply this
    Cwd      string
    ExitCode *int
    StartedAt, EndedAt time.Time
    Source   string    // provenance: which provider produced this
    Fidelity Fidelity // Lossless | Lossy | Heuristic
}
```

`Capture.Command` being filled from L1, not the provider, is deliberate —
it's the concrete payoff of running both layers.

**Provider status (verified on this machine vs. needs-verification):**

| Provider | Mechanism | Status |
|---|---|---|
| `kitty` | `kitten @ get-text --extent last_cmd_output` | ✅ verified on 0.49.2 |
| `tmux` | `capture-pane -p -S -`, `#{pane_current_command}` | ✅ tmux installed, standard |
| `zellij` | `zellij action dump-screen` | ⚠️ installed, API needs verification |
| `pty-tap` | own pty proxy | ⬜ to build — the universal one |
| `wezterm` | `wezterm cli get-text --pane-id N` | ❓ needs verification |
| `ghostty` | unix-socket RPC `get-text` | ❓ needs verification |
| `iterm2` | AppleScript / Python API | ❓ macOS only, likely weak |
| `konsole` | DBus — no public capture API | ❓ likely unusable |
| `alacritty`, `foot`, `gnome-terminal`, `wt`, `vscode` | — | ❌ none; tap covers these |

---

## 3. Data layout

```
$XDG_STATE_HOME/wh/sessions/<sid>/
    events.jsonl      # L1 markers, one line per boundary event
    stream.ansi       # L0 raw bytes, append-only, rotated at N MB

<save-dir>/<YYYYmmdd-HHMMSS>-<slug>/
    meta.json         # cmd, cwd, exit, duration, source, fidelity, host, tty
    output.ansi       # faithful, replayable
    output.txt        # ANSI-stripped, clean
    README.md         # human-readable: command as header, then output
    (optional) meta redactions applied
```

`wh save` = look up last `command_done` in `events.jsonl`, take the byte range,
render both forms, write the directory. Cheap, and it works for commands that
have already scrolled off the screen.

---

## 4. Concerns you asked me to raise

**1. Your `sed '$d'` is fragile, and not only for the reason above.**
It assumes (a) exactly one trailing line is the new prompt, (b) the prompt is
one line, (c) the command has finished. Multi-line prompts (p10k, fish, PS2),
a command still running, or an output ending in `\n\n` all break it. The
`--extent last_cmd_output` equivalents use markers and don't care.

**2. Snapshot vs. history — decide this now, it changes the architecture.**
A keypress-time snapshot can only ever capture what's still in the scrollback
ring. Your kitty has `scrollback_lines 5000`; anything beyond that is *gone*.
A continuous recorder never loses it. Strong recommendation: **record always,
save on demand.** Record is append-only and cheap; save is the deliberate act.
This is the single biggest design decision and it's cheap to get right now,
expensive to retrofit.

**3. TUIs (vim, htop, fzf, less) are a tier-2 citizen in every design.**
They draw to the *alternate screen*. A snapshot of a TUI gives you a frame, not
a transcript — and when the program exits, kitty restores the primary screen and
the content is unrecoverable via `get-text`. The PTY tap at least preserves the
byte stream (replayable, `asciinema play`-able), but not "clean text".
Decide explicitly: is the artifact *clean text to read* or *faithful replay*?
Note they're different products and it costs ~2× storage to keep both.
Recommendation: store `output.ansi` as the source of truth, render `output.txt`
on demand.

**4. ANSI, wrapping, and `\r`.** Without `--ansi` you get clean text and lose
color. With it you keep the stream but the emulator has *already* applied soft
wraps — you need `--add-wrap-markers` to know where a logical line ended.
Progress bars collapse to one line in clean mode. All rendering decisions;
state them rather than discover them later.

**5. The screen lies about the command.** What's rendered is the *drawn* command
line — mangled by multi-line prompts, right-aligned prompts, prompt plugins.
The shell hook knows the real string (`$history[1]`, or the accepted line at
`zle` accept time). Another concrete win for the dual-layer design.

**6. Multiplexers break terminal identity.** Inside tmux, kitty sees a window
containing the tmux *client*, not the pane. You must slice per-pane. Same for
zellij. This is a common enough case to design for up front, not bolt on.

**7. SSH splits your session in half.** The shell hook fires on the *remote*
host; your capture tap is *local*. You'll get a local command `ssh host 'x'`
with the remote output attributed to it, plus markers from a host you can't
see. Needs an explicit `host` + session-id in metadata, and a rule about what
a "command" means across a boundary. Undefined behaviour here will feel like a
bug later.

**8. Windows is a genuinely different world.** No pty, ConPTY swallows output,
Windows Terminal has no capture API, and PowerShell can only read the visible
buffer via Win32 in legacy console mode. **Recommend: POSIX-first, and don't
promise Windows in v1.** Pretending otherwise costs you the clean architecture.

**9. You're writing secrets to disk, permanently.** Terminal output is where
`AWS_SECRET_ACCESS_KEY`, `cat .env`, and bearer tokens end up. Non-negotiables:
`0700`/`0600` perms, an obviously-scoped path under XDG_STATE_HOME (not the
project dir), a `.gitignore` story, opt-in redaction (e.g. `(?i)(token|key|secret)
=\S+`), and a retention policy. Also worth knowing: your `allow_remote_control
yes` (kitty.conf:28) is fine — the per-user unix socket is safe — but never
expose a TCP `listen_on` without the password.

**10. What is the artifact, really?** One file? A directory per command? Which
directory — per-project, per-session, one global archive? Naming scheme? A
searchable index? These are cheap to decide now and annoying later. The sketch
above proposes one; confirm or change it.

**11. Combinatorial maintenance.** N terminals × M shells × K OSes. Keep each
provider to a single file with 3 methods and no shared state, so it can rot
independently and never crash the recorder. Prefer "no API, use the tap" over
a fragile integration.

**12. Your directory name ends in `?`.** `/Projects/WhatHappened?` needs quoting
in every shell command and breaks naive path handling. Consider
`whathappened` for the repo, `wh` for the binary.

---

## 5. Milestones

**M0 — kitty only, ~1 day.** `wh save` shelling out to `kitten @ get-text
--extent last_cmd_output`, saving a directory. Deliberately disposable: it
replaces your `sed` hack and forces you to settle the *artifact shape* — file
names, what goes in `meta.json`, ANSI or not. Every provider downstream depends
on those answers, so settle them before writing the hard parts.

**M1 — the PTY tap + shell markers.** `wh wrap zsh` + bash/zsh/fish snippets
emitting OSC 133 and side-channel JSONL. This is the actual "works everywhere"
answer and the bulk of the real work.

**M2 — tmux/zellij providers**, so multiplexed sessions slice per-pane.

**M3 — wezterm/ghostty/iterm2** as fidelity upgrades. Verify each API before
committing; drop any that turn out to be unmaintainable.

**M4 — the store:** save dir conventions, index, search, redaction, retention.

**Open question for you:** language. Go is my recommendation (single static
binary, `creack/pty`, easy to vendor as a terminal-adjacent tool); Rust if you
want it tighter; Python is fine for M0 only and probably painful for M1.

**Open question for you:** does `wh save` stay a manual keybind/command, or do
you also want auto-save policies (every command? only failures? only
non-empty output?)?
