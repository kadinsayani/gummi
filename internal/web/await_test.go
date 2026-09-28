package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/morphis/gummi/internal/ui"
	"github.com/morphis/gummi/internal/webapi"
)

// A question the flow stopped at — an input it needs, a confirmation, a
// line that reads as a new card — is not an HTTP error: it is answered
// 202 with the same body a refusal carries, so no browser logs it as a
// failed load. A refusal (moved, answered, busy, not offered) stays a 409,
// and a bad request a 400.
func TestAQuestionIsNotAnHTTPError(t *testing.T) {
	draft := "FD-001: dark mode"
	for _, tc := range []struct {
		name string
		err  *ui.WebError
		want int
	}{
		{"needs", &ui.WebError{Code: ui.WebConflict, Reason: webapi.ConflictNeeds, Needs: string(webapi.ActionNeedsMessage), Text: "read the landing message", Draft: &draft}, webapi.StatusQuestion},
		{"confirm", &ui.WebError{Code: ui.WebConflict, Reason: webapi.ConflictConfirm, Needs: string(webapi.ActionNeedsConfirm), Text: "delete FD-001?", Confirm: "c123"}, webapi.StatusQuestion},
		{"newcard", &ui.WebError{Code: ui.WebConflict, Reason: webapi.ConflictNewCard, Text: "a separate line"}, webapi.StatusQuestion},
		{"moved", &ui.WebError{Code: ui.WebConflict, Reason: webapi.ConflictMoved, Text: "the card moved since you read it"}, http.StatusConflict},
		{"answered", &ui.WebError{Code: ui.WebConflict, Reason: webapi.ConflictAnswered, Text: "someone answered it first", By: "alice"}, http.StatusConflict},
		{"busy", &ui.WebError{Code: ui.WebConflict, Reason: webapi.ConflictBusy, Text: "a line"}, http.StatusConflict},
		{"refused", &ui.WebError{Code: ui.WebConflict, Text: "merge is not offered on FD-001 right now"}, http.StatusConflict},
		{"bad request", &ui.WebError{Code: ui.WebBadRequest, Text: "no option \"x\" on this decision"}, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			(&Server{}).fail(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			var body webapi.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			want := tc.err.Reason
			if want == "" {
				want = tc.err.Text
			}
			if body.Error != want || (tc.err.Reason != "" && body.Text != tc.err.Text) || body.Confirm != tc.err.Confirm {
				t.Errorf("body = %+v, want the %s shape", body, tc.name)
			}
			if webapi.IsQuestion(body.Error) != (tc.want == webapi.StatusQuestion) {
				t.Errorf("IsQuestion(%q) disagrees with the status", body.Error)
			}
		})
	}
}
