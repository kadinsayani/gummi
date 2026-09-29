package engine

import (
	"fmt"
	"strings"
)

// A stage session whose tool calls keep failing is stuck, and nothing
// else stops it. The budget cap only bites when the model costs
// something, and the driver's --stage-timeout only when the session goes
// quiet. A model retrying a call its backend refuses is neither: it
// ticks along at a step every few seconds, for free if the model is free,
// until a person notices. One did, 600 steps and 45 minutes into a
// verify stage whose every call was an opencode permission denial.
//
// A refused call cannot start succeeding on a retry, so the guard is
// about calls that do not change: the same call failing
// toolLoopRepeatCap times, with nothing succeeding in between, ends the
// run. toolLoopStreakCap is the backstop for a model that varies the
// call a little each time: that many failures in a row, whatever they
// were, with no success among them. Both reset on any call that works,
// so a session working through a string of failing edits is untouched.
const (
	toolLoopRepeatCap = 5
	toolLoopStreakCap = 20
)

// toolFailStreak is the run of failed tool calls since the last one
// that succeeded. It lives on Session, under Session.mu.
type toolFailStreak struct {
	total int
	calls map[string]int // per call signature, within the streak
}

// errToolLoop is the error a tripped guard fails the run with: it names
// the call and what the backend said about it, which is what the person
// reading the stopped card has to go on.
type errToolLoop struct {
	tool, detail, output string
	repeats, streak      int
}

func (e *errToolLoop) Error() string {
	call := e.tool
	if e.detail != "" {
		call += " " + e.detail
	}
	why := firstLine(e.output)
	if why == "" {
		why = "no reason given"
	}
	if e.repeats >= toolLoopRepeatCap {
		return fmt.Sprintf("stopped a looping agent: the same tool call failed %d times in a row (%s): %s — "+
			"a refused call does not start working on a retry; if the backend's permissions deny it, "+
			"that is where the fix belongs", e.repeats, call, why)
	}
	return fmt.Sprintf("stopped a looping agent: %d tool calls in a row failed with none succeeding (last: %s): %s",
		e.streak, call, why)
}

// noteToolOutcome folds one finished tool call into the session's
// failure streak and reports the error to fail the run with once the
// streak shows the session is looping, nil otherwise.
func (s *Session) noteToolOutcome(tool, detail string, ok bool, output string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		s.failing = toolFailStreak{}
		return nil
	}
	if s.failing.calls == nil {
		s.failing.calls = map[string]int{}
	}
	s.failing.total++
	// the output is part of the call's identity: the same command failing
	// for a different reason is progress of a kind, not a loop
	sig := tool + "\x00" + detail + "\x00" + output
	s.failing.calls[sig]++
	n := s.failing.calls[sig]
	if n < toolLoopRepeatCap && s.failing.total < toolLoopStreakCap {
		return nil
	}
	return &errToolLoop{tool: tool, detail: detail, output: output, repeats: n, streak: s.failing.total}
}

// firstLine returns s's first non-blank line, trimmed and cut to a
// length a card's error line can carry: opencode's denial is one line
// that goes on to list every permission rule in force.
func firstLine(s string) string {
	const maxRunes = 200
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if r := []rune(l); len(r) > maxRunes {
				l = string(r[:maxRunes]) + "…"
			}
			return l
		}
	}
	return ""
}
