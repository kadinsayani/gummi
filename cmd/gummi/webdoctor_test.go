package main

import (
	"encoding/json"
	"testing"
)

// The web face's doctor is `gummi doctor --json`'s checklist, item for
// item: the page is a second place to read it, never a second checklist.
func TestWebDoctorIsTheCLIChecklist(t *testing.T) {
	clearDoctorEnv(t)
	r := buildDoctorReport(gitRepo(t), doctorOpts{})
	web := doctorForWeb(r)
	if web.Ready != r.Ready || len(web.Checks) != len(r.Checks) {
		t.Fatalf("web doctor has %d checks (ready %v), the CLI %d (ready %v)", len(web.Checks), web.Ready, len(r.Checks), r.Ready)
	}
	cli, err := json.Marshal(r.Checks)
	if err != nil {
		t.Fatal(err)
	}
	page, err := json.Marshal(web.Checks)
	if err != nil {
		t.Fatal(err)
	}
	var a, b []map[string]any
	if err := json.Unmarshal(cli, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(page, &b); err != nil {
		t.Fatal(err)
	}
	for i := range a {
		for _, k := range []string{"name", "status", "detail", "remediation"} {
			if a[i][k] != b[i][k] {
				t.Errorf("check %d %s: CLI %v, web %v", i, k, a[i][k], b[i][k])
			}
		}
	}
}
