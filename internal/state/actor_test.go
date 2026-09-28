package state

import "testing"

func TestPersonActor(t *testing.T) {
	cases := []struct {
		actor  string
		person bool
		name   string
	}{
		{ActorUser, true, ""},
		{PersonActor("Simon"), true, "Simon"},
		{PersonActor("  "), true, ""},
		{ActorAutopilot, false, ""},
		{"caller", false, ""},
		{"auto", false, ""},
		{"username", false, ""},
	}
	for _, c := range cases {
		if got := IsPersonActor(c.actor); got != c.person {
			t.Errorf("IsPersonActor(%q) = %v, want %v", c.actor, got, c.person)
		}
		if got := PersonName(c.actor); got != c.name {
			t.Errorf("PersonName(%q) = %q, want %q", c.actor, got, c.name)
		}
	}
	if PersonActor("Simon") != "user:Simon" {
		t.Errorf("PersonActor(Simon) = %q", PersonActor("Simon"))
	}
}
