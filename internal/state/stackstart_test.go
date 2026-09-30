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

// Deleting a card takes it out of its stack like `stack rm` does: the
// cards above move down a place, so positions stay contiguous.
func TestDeletingAStackedCardClosesTheGap(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	for i, slug := range []string{"a", "b", "c"} {
		f := domain.Feature{ID: domain.FeatureID("FD-00" + string(rune('1'+i))), Num: i + 1, Title: slug, Slug: slug, Kind: domain.KindFeature, Stage: domain.StageTodo}
		if err := s.CreateFeature(ctx, &f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.StartStack(ctx, "FD-001", "chain", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.FeatureID{"FD-002", "FD-003"} {
		if err := s.AddToStack(ctx, "chain", id, 99); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteFeature(ctx, "FD-002"); err != nil {
		t.Fatal(err)
	}
	cards, err := s.ListStackCards(ctx, "chain")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].StackPos != 0 || cards[1].ID != "FD-003" || cards[1].StackPos != 1 {
		t.Fatalf("stack after the delete = %+v, want FD-001@0 and FD-003@1", cards)
	}
}

// A card whose work has landed or been handed off holds its place: lifting
// it above open cards would name a closed card as what they fork from.
func TestAClosedCardCannotBeMovedInItsStack(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	for i, slug := range []string{"a", "b"} {
		f := domain.Feature{ID: domain.FeatureID("FD-00" + string(rune('1'+i))), Num: i + 1, Title: slug, Slug: slug, Kind: domain.KindFeature, Stage: domain.StageTodo}
		if err := s.CreateFeature(ctx, &f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.StartStack(ctx, "FD-001", "chain", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AddToStack(ctx, "chain", "FD-002", 99); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE features SET stage = 'done' WHERE id = 'FD-001'`); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveInStack(ctx, "FD-001", 1); !errors.Is(err, ErrStackCardClosed) {
		t.Fatalf("moving a done card: %v, want ErrStackCardClosed", err)
	}
	if err := s.MoveInStack(ctx, "FD-002", 0); err != nil {
		t.Fatalf("moving an open card below a closed one: %v", err)
	}
}
