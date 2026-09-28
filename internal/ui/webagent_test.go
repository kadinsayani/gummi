package ui

import (
	"testing"

	"github.com/morphis/gummi/internal/engine"
)

// The board agent's live block says what it is doing in the words the
// TUI's board thread shows under its spinner, as a card's live block
// does with the card's busy word.
func TestTheBoardAgentsLiveBlockSaysWhatItIsDoing(t *testing.T) {
	live := webBoardLive(engine.Snapshot{Busy: true}, "", nil)
	if live == nil || !live.Busy || live.Verb != boardBusyWord(engine.Snapshot{Busy: true}) || live.Verb == "" {
		t.Fatalf("busy board agent live = %+v, want the TUI's busy word", live)
	}
	if idle := webBoardLive(engine.Snapshot{SpentCredits: 2}, "", nil); idle == nil || idle.Verb != "" {
		t.Fatalf("idle board agent live = %+v, want no verb", idle)
	}
	if none := webBoardLive(engine.Snapshot{}, "", nil); none != nil {
		t.Fatalf("a board agent that has done nothing has a live block: %+v", none)
	}
}
