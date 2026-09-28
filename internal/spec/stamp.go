package spec

import (
	"crypto/sha1" //nolint:gosec // git's own object id, not a security boundary
	"encoding/hex"
	"strconv"
	"strings"
)

// Rev is a document's revision as git names the same bytes: the blob id
// `git hash-object` prints. A page that read the document at one Rev can
// say "this changed" by comparing it, and the id matches the one the
// committed copy will have.
func Rev(content []byte) string {
	h := sha1.New() //nolint:gosec // see the import
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// A marker's parenthesis carries its date, and — for a note a named
// person wrote from a device rather than the terminal — the person too:
//
//	%% @user(2026-09-27, Simon): name the flag -n
//
// The author stays `user`, because that word is what makes a comment a
// human's to every rule that reads markers (a gate held open, a thread
// only a person may close); the name rides along in the date slot, which
// the grammar keeps verbatim and nothing else reads.

// Stamp is a marker's parenthesis for date and, when person is set, the
// person who wrote it. The person's name is kept to what the slot can
// hold: no parentheses, no line breaks, no comma.
func Stamp(date, person string) string {
	person = strings.Map(func(r rune) rune {
		switch r {
		case '(', ')', ',', '\n', '\r':
			return -1
		}
		return r
	}, person)
	person = strings.TrimSpace(person)
	if person == "" {
		return date
	}
	return date + ", " + person
}

// SplitStamp takes a marker's Date back apart into the date and the
// person Stamp put there ("" when the marker names nobody).
func SplitStamp(stamp string) (date, person string) {
	date, person, _ = strings.Cut(stamp, ",")
	return strings.TrimSpace(date), strings.TrimSpace(person)
}
