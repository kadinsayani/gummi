package ui

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/spec"
)

// TestACleanCritiqueHeldByAnOpenCommentSaysSo: a clean critique whose
// gate a person's open comment still holds shut is raised naming the
// comment, not inviting an approval the gate refuses; once the comment is
// resolved the reason says the gate is clear.
func TestACleanCritiqueHeldByAnOpenCommentSaysSo(t *testing.T) {
	ctx := context.Background()
	m, eng := agentWorkspace(t, writingArchitect())
	if err := m.store.SetGateApproval(ctx, "FD-001", domain.GateAttended); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, m.loadRows)
	draftRequiredSections(t, m)
	f := m.rows[0].F
	path := m.artifactFile(&f)
	if path == "" {
		t.Fatal("fixture: no artifact")
	}
	edit := func(line string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := spec.ViewSection(string(raw), "Problem")
		next, _, err := spec.ReplaceSection(string(raw), "Problem", body+line+"\n")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	edit("It must wave.\n%% @user: say what waving means")

	m = openAndAttach(t, m)
	settleChat(t, eng)
	m = drainEngineLoop(t, m)
	it, ok := m.inbox.get("FD-001")
	if !ok || it.Kind != attnGate {
		t.Fatalf("no gate raised: %+v", it)
	}
	if !strings.Contains(it.Text, "1 open comment in the spec") || strings.Contains(it.Text, "review & approve") {
		t.Fatalf("gate reason = %q, want it to name the open comment", it.Text)
	}

	f = m.rows[0].F
	path = m.artifactFile(&f) // the run moved it to its workspace home
	edit("%% @user: resolved — a hand raised and moved side to side")
	m = pump(t, m, m.loadRows)
	if it, _ := m.inbox.get("FD-001"); heldByComments(it.Text) {
		t.Errorf("gate reason still names resolved comments: %q", it.Text)
	}
}
