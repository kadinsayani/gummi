package ui

// Drawing the autopilot stretch: the rules that bracket a period a card
// spent being driven without you, and the decisions it made inside one.
// What a period is, where it begins and how it ends is the fold's
// business (internal/threadfold's Stretches); this file only says it in
// the terminal's furniture.

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/morphis/gummi/internal/state"
	"github.com/morphis/gummi/internal/threadfold"
	"github.com/morphis/gummi/internal/ui/theme"
)

// stretchOpenLine is the rule that opens a period. It is drawn at
// s.Subtle where an ordinary rule is s.Faint — one step brighter and
// nothing more. The period needs to be findable while paging back
// through a long card, and two rules a shade above the furniture do that
// without spending a colour on it, which is what makes the whole thing
// affordable: no new hue, no pinned region, nothing to clean up.
func stretchOpenLine(s *theme.Styles, st threadfold.Stretch, w int) string {
	return s.Subtle.Render(stretchRule(threadfold.StretchOpenLabel, st.OpenedAt, w))
}

// stretchCloseLines are the rule that closes a period and the two lines
// under it. The reason and the tally each take their own row rather than
// riding the rule: the full wording measures 85 columns against an
// 84-column pane before any narrow terminal is considered, so putting
// them on the rule would mean one wording at wide widths and another at
// narrow ones — two things to render, two to test, and a rule that reads
// differently depending on the window.
//
// Either row is withheld when it has nothing to say. A period that
// crossed no gate and answered no question still draws its rules, since
// "it ran implement while you were out" is worth saying; it is only the
// tally that would be a row of zeroes, and that row alone goes.
func stretchCloseLines(s *theme.Styles, st threadfold.Stretch, w int) []string {
	out := []string{s.Subtle.Render(stretchRule(threadfold.StretchLabel(st.Closed), st.ClosedAt, w))}
	if st.Reason != "" {
		out = append(out, "  "+s.Subtle.Render(ansi.Truncate(sanitize(st.Reason), max(w-2, 8), "…")))
	}
	if !st.DecidedNothing() {
		out = append(out, "  "+s.Subtle.Render(st.Tally()))
	}
	return out
}

// stretchRule is boundaryRule's dash-fill shape (thread.go) for a
// stretch boundary: a label on the left, the time on the right, dashes
// between. It carries no role or model, because a period is not a
// session — it can span several.
func stretchRule(label string, at time.Time, w int) string {
	head := "── " + label + " "
	tail := "──"
	if !at.IsZero() {
		tail = " " + at.Format("15:04") + " ──"
	}
	fill := max(w-ansi.StringWidth(head)-ansi.StringWidth(tail), 0)
	return head + strings.Repeat("─", fill) + tail
}

// stretchDecisionLine renders one crossing or answer autopilot made,
// pulled out of the fold so it keeps the position it happened at.
// Folding a stage to a receipt drops everything inside it, which is
// exactly why the block this replaces existed: with the decisions gone
// from the history, the only place left to report them was a rollup at
// the end. Keeping them here is what makes the rollup unnecessary. What
// the line says, and whose it is, is threadfold.DecisionLine's.
func stretchDecisionLine(s *theme.Styles, ev state.CardEvent, inStretch bool, w int) string {
	line := threadfold.DecisionLine(ev, inStretch)
	if line == "" {
		return ""
	}
	return stampedLine(s, line, ev.At, w)
}

// stampedLine is one pulled decision: a tick, the sentence, and the time
// pushed to the right margin so a run of them reads as a column. The
// stamp is dropped rather than truncated when the sentence needs the
// room — the time is the least of what the line is saying.
func stampedLine(s *theme.Styles, line string, at time.Time, w int) string {
	body := s.Success.Render("✓ ") + s.Subtle.Render(sanitize(line))
	stamp := ""
	if !at.IsZero() {
		stamp = at.Format("15:04")
	}
	pad := w - 2 - ansi.StringWidth(sanitize(line)) - ansi.StringWidth(stamp) - 1
	if stamp == "" || pad < 1 {
		return ansi.Truncate(body, w, "…")
	}
	return body + strings.Repeat(" ", pad) + s.Faint.Render(stamp)
}

// newestSeq is the high-water mark of an event slice, or 0 for an empty
// one — what gets recorded as read once a card has been looked at.
func newestSeq(events []state.CardEvent) int64 {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Seq
}
