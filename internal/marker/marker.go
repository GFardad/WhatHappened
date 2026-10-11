// Package marker defines the prompt-boundary protocol shared by shell
// integration (the producer), the PTY tap (the byte-offset accountant), and the
// session store (the consumer).
//
// This file is a CONTRACT, authored up front so independent packages can be
// developed in parallel. Types and constants only — no behaviour here.
//
// A shell emits every boundary event TWICE, from one hook:
//
//  1. In-band, as OSC 133 escape sequences. Emulators that understand them
//     (kitty, WezTerm, Ghostty, foot, modern VTE) can then answer "give me the
//     last command's output" natively — e.g. kitty's
//     `kitten @ get-text --extent last_cmd_output`.
//
//  2. Out-of-band, appended to a per-session JSONL file. This never has to reach
//     the emulator, so it works in terminals that ignore or strip OSC 133
//     (Windows Terminal, VS Code, ConEmu, older VTE) and under a PTY tap.
//
// That dual emission is what makes the tool universal without the shell knowing
// which terminal it is running inside.
package marker

import "time"

// Type identifies a boundary event in the command lifecycle.
//
// OSC 133 only defines four marks (A/B/C/D); the session types below are
// side-channel-only and have no escape-sequence representation.
type Type string

const (
	// PromptStart is emitted when the shell draws a prompt. OSC 133 mark A.
	PromptStart Type = "prompt_start"

	// CommandStart is emitted when the user accepts a line and the command
	// begins. OSC 133 mark B. Carries Cmd.
	CommandStart Type = "command_start"

	// CommandOutput is emitted immediately after the command line, before any
	// output is produced. OSC 133 mark C.
	CommandOutput Type = "command_output"

	// CommandDone is emitted when the command has finished and the next prompt
	// is about to be drawn. OSC 133 mark D. Carries ExitCode.
	CommandDone Type = "command_done"

	// SessionStart is side-channel only: written once when a session directory
	// is created.
	SessionStart Type = "session_start"

	// SessionEnd is side-channel only: written once when the recorder stops.
	SessionEnd Type = "session_end"
)

// Event is one record in the session's events.jsonl.
//
// ByteStart and ByteEnd are byte offsets into the session's stream.ansi file.
// They are only meaningful when a recorder (the PTY tap) is attaching them; the
// side-channel emitter sets them to -1 when it cannot know them.
type Event struct {
	// Seq is a monotonic counter within one session, starting at 1. It lets a
	// consumer detect gaps without parsing timestamps.
	Seq int64 `json:"seq"`

	// Type is the lifecycle event.
	Type Type `json:"type"`

	// Cmd is the command line, set on CommandStart.
	Cmd string `json:"cmd,omitempty"`

	// Cwd is the working directory, set on CommandStart and SessionStart.
	Cwd string `json:"cwd,omitempty"`

	// ExitCode is set on CommandDone. Nil means the command was killed or the
	// status is unknown.
	ExitCode *int `json:"exit,omitempty"`

	// TTY is the controlling terminal device, used to disambiguate sessions.
	TTY string `json:"tty,omitempty"`

	// PID is the shell process id, used to disambiguate sessions.
	PID int `json:"pid,omitempty"`

	// Host is the hostname. Set by the recorder; empty for purely local shells.
	// A command that crosses an SSH boundary cannot be attributed from one host
	// alone, so this is what lets a consumer detect that case.
	Host string `json:"host,omitempty"`

	// TS is the wall-clock time the event was produced.
	TS time.Time `json:"ts"`

	// ByteStart is the offset in stream.ansi where this command's output began.
	// -1 when unknown.
	ByteStart int64 `json:"byte_start"`

	// ByteEnd is the offset in stream.ansi where this command's output ended.
	// -1 when unknown.
	ByteEnd int64 `json:"byte_end"`
}

// OSC 133 semantic prompt marks, terminated with BEL.
//
// These are deliberately NOT written into stream.ansi's saved output. They are
// control noise for the consumer: the recorder strips them from the ranges it
// hands to providers, so saved artefacts contain only real program output.
const (
	// PromptStartA marks the start of a prompt: "\x1b]133;A\x07".
	PromptStartA = "\x1b]133;A\x07"

	// CommandStartB marks the start of a command: "\x1b]133;B\x07".
	CommandStartB = "\x1b]133;B\x07"

	// CommandOutputC marks the start of command output: "\x1b]133;C\x07".
	CommandOutputC = "\x1b]133;C\x07"

	// CommandDoneD marks the end of a command: "\x1b]133;D\x07".
	CommandDoneD = "\x1b]133;D\x07"
)

// CommandDoneDWithExit renders mark D carrying the exit status, which some
// emulators use. Format: "\x1b]133;D;<exit>\x07".
func CommandDoneDWithExit(code int) string {
	return "\x1b]133;D;" + itoa(code) + "\x07"
}

// UnknownOffset is the sentinel for "no byte offset known", used in Event.
const UnknownOffset int64 = -1

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
