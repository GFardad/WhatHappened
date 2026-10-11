// Package capture defines the shared contracts between the capture providers,
// the session store, and the CLI.
//
// This file is a CONTRACT, authored up front so that independent packages can be
// developed in parallel without agreeing on names at runtime. It contains types
// and documentation only — no behaviour lives here.
//
// Design rule: a terminal can tell us what bytes were rendered, but only a shell
// can tell us what command produced them. Therefore Capture.Command is populated
// from shell integration metadata (see package marker), never from a provider's
// own guess.
package capture

import (
	"context"
	"time"
)

// Fidelity describes how trustworthy a Capture is.
type Fidelity string

const (
	// FidelityLossless means the bytes are exact and the command boundaries are
	// known precisely (PTY tap with shell markers).
	FidelityLossless Fidelity = "lossless"

	// FidelityLossy means the text was rendered by the terminal emulator, so
	// styling and soft wrapping have already been applied, but the command
	// boundaries are known precisely (kitty --extent last_cmd_output, tmux
	// capture-pane with pane markers).
	FidelityLossy Fidelity = "lossy"

	// FidelityHeuristic means the command boundaries were guessed, e.g. by
	// looking for a trailing prompt line. Treat results as best-effort.
	FidelityHeuristic Fidelity = "heuristic"
)

// Scope selects what a provider should capture.
type Scope string

const (
	// ScopeLastCommand asks for the most recent completed command and its output.
	ScopeLastCommand Scope = "last_command"

	// ScopeAll asks for the whole available screen/scrollback.
	ScopeAll Scope = "all"

	// ScopeAlternate asks for the alternate screen buffer (TUI applications).
	ScopeAlternate Scope = "alternate"

	// ScopeRunning asks for the command currently executing, including output so
	// far. Best-effort; may be unavailable on some providers.
	ScopeRunning Scope = "running"
)

// Request is the argument to Provider.Capture.
type Request struct {
	// Scope selects what to capture. Empty means ScopeLastCommand.
	Scope Scope

	// TTY identifies the terminal device (e.g. "/dev/pts/7"). Providers that
	// multiplex may ignore it and select the active pane instead.
	TTY string

	// WindowID is the terminal's own window identifier when it exports one
	// (kitty sets KITTY_WINDOW_ID). Empty when unknown.
	WindowID string

	// MaxBytes caps the returned payload. Zero means provider default.
	MaxBytes int
}

// Capture is one command's worth of recorded terminal output plus its metadata.
//
// Raw is the source of truth: it preserves ANSI colour and cursor movement so
// the capture can be replayed. Any human-readable form (plain text, markdown) is
// rendered from Raw on demand, never stored as the primary artefact.
type Capture struct {
	// Raw is the faithful byte stream of the command output, ANSI preserved.
	Raw []byte

	// Command is the command line that produced this output. Sourced from shell
	// integration metadata when available; empty otherwise. Never invent this
	// from scraped screen text — a rendered prompt is unreliable.
	Command string

	// Cwd is the working directory the command ran in, when known.
	Cwd string

	// ExitCode is the command's exit status. Nil when unknown or still running.
	ExitCode *int

	// StartedAt and EndedAt bound the command. Zero values mean unknown.
	StartedAt time.Time
	EndedAt   time.Time

	// Source names the provider that produced this capture, for provenance.
	Source string

	// Fidelity records how much the provider can be trusted.
	Fidelity Fidelity

	// Notes carries human-readable caveats, e.g. "output truncated, scrollback
	// limit reached" or "terminal reported no shell integration".
	Notes []string
}

// Provider captures terminal output from one specific source.
//
// Implementations MUST be independent: a provider may fail, time out, or return
// an error, and must never panic or corrupt shared state. The registry picks the
// best available provider at runtime, so providers must not depend on each other.
type Provider interface {
	// Name is a stable short identifier, e.g. "kitty", "tmux", "pty-tap".
	Name() string

	// Available reports whether this provider can be used right now, in this
	// process, against the given request target. It must be cheap and must not
	// block; it may probe for a socket or binary but must not perform a capture.
	Available(req Request) bool

	// Fidelity reports the best fidelity this provider can reach, used to rank
	// providers before attempting a capture.
	Fidelity() Fidelity

	// Capture retrieves output according to req.
	Capture(ctx context.Context, req Request) (Capture, error)
}
