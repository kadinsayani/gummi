package state

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/morphis/gummi/internal/domain"
)

func TestStartStackNamesItAfterTheBottomCard(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	mk := func(id domain.FeatureID, num int, slug string, kind domain.Kind) {
		t.Helper()
		f := domain.Feature{ID: id, Num: num, Title: slug, Slug: slug, Kind: kind, Stage: domain.StageTodo}
		if err := s.CreateFeature(ctx, &f); err != nil {
			t.Fatal(err)
		}
	}
	mk("FD-001", 1, "theme", domain.KindFeature)
	mk("FD-002", 2, "theme", domain.KindFeature)
	mk("GL-003", 3, "ship-it", domain.KindGoal)

	st, err := s.StartStack(ctx, "FD-001", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if st.ID != "theme" || st.Name != "theme" {
		t.Fatalf("stack = %+v, want id and name from the slug", st)
	}
	// a second card with the same slug still gets a stack of its own
	st2, err := s.StartStack(ctx, "FD-002", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if st2.ID != "fd-002-theme" {
		t.Fatalf("collision id = %q, want the bottom card's id in front", st2.ID)
	}
	if _, err := s.StartStack(ctx, "FD-001", "again", time.Now()); err == nil {
		t.Fatal("a card already in a stack started another")
	}
	if _, err := s.StartStack(ctx, "GL-003", "", time.Now()); !errors.Is(err, ErrStackNotStackable) {
		t.Fatalf("a goal started a stack: %v", err)
	}
	stacks, err := s.ListStacks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stacks) != 2 {
		t.Fatalf("stacks = %+v, want the two that started and nothing left by the refusals", stacks)
	}
}
