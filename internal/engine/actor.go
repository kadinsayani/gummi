package engine

import (
	"context"

	"github.com/morphis/gummi/internal/state"
)

// actorKey carries, on a send's context, who typed the line: the actor the
// store records a person as (state.PersonActor). A board shared by several
// people (the web face, DESIGN §20.3) names each line's author from it; the
// terminal sends none, and its lines stay the plain "you" they always were.
type actorKey struct{}

// WithActor returns ctx carrying actor as the author of what is sent on it.
func WithActor(ctx context.Context, actor string) context.Context {
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorKey{}, actor)
}

// actorOf is the actor WithActor put on ctx, or "".
func actorOf(ctx context.Context) string {
	a, _ := ctx.Value(actorKey{}).(string)
	return a
}

// personOf is the person a hand-driven act on ctx is recorded as: the
// actor WithActor put there (a named person from the web face), or the
// terminal's bare "user".
func personOf(ctx context.Context) string {
	if a := actorOf(ctx); a != "" {
		return a
	}
	return state.ActorUser
}
