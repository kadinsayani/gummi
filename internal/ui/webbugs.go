package ui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/engine"
	"github.com/morphis/gummi/internal/webapi"
)

// Bug import on the web face (DESIGN §12.4). The TUI's picker takes one
// issue into the new-card form; the web face does what `gummi bugs
// ingest` does instead — list the issues, then mint the ones chosen
// straight into todo — because a page can offer a checklist where a
// terminal offers a cursor. Both paths are Engine.IngestBugs and
// Engine.MaterializeBugs; gh runs through GUMMI_GH_CMD like every other
// gh call gummi makes.

// BugQuery is GET /api/bugs's parameters: the GitHub owner/repo (empty
// lets gh read the checkout's remote), the label and state filters, how
// many, and In, the managed repository whose checkout gh runs in.
type BugQuery struct {
	Repo     string
	Label    string
	State    string
	In       string
	Limit    int
	Comments bool
}

// webBugFetch picks up what a fetch needs: the engine's IngestBugs (or a
// test's stub, the seam the TUI's picker uses) and the checkout gh runs
// in.
func (m *Shell) webBugFetch(in string) (func(context.Context, engine.GitHubSource) (engine.BugIngestResult, error), string, error) {
	if m.engine == nil {
		return nil, "", webErr(WebUnavailable, "%s", m.noAgent(" — bug import needs the engine"))
	}
	fetch := m.ghIssues
	if fetch == nil {
		eng := m.engine
		fetch = func(ctx context.Context, src engine.GitHubSource) (engine.BugIngestResult, error) {
			return eng.IngestBugs(ctx, src)
		}
	}
	dir, _ := m.repoRoot(in)
	if dir == "" && m.wt != nil {
		dir = m.wt.Root()
	}
	return fetch, dir, nil
}

// Bugs is GET /api/bugs: the issues an import would propose, and the ones
// already on the board. A gh failure is the answer's Error, not a failed
// request: the page says why the list is empty.
func (b *Bridge) Bugs(ctx context.Context, q BugQuery) (webapi.Bugs, error) {
	return webLoad(ctx, b, func(m *Shell) (func(context.Context) (webapi.Bugs, error), error) {
		fetch, dir, err := m.webBugFetch(q.In)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context) (webapi.Bugs, error) {
			src := engine.GitHubSource{Repo: q.Repo, Label: q.Label, State: q.State, Dir: dir, Limit: q.Limit, FetchComments: q.Comments}
			res, err := fetch(ctx, src)
			out := webapi.Bugs{Source: src.Name(), Proposals: []webapi.BugProposal{}}
			if err != nil {
				out.Error = sanitize(err.Error())
				return out, nil
			}
			for _, p := range res.Proposals {
				out.Proposals = append(out.Proposals, webBugProposal(p))
			}
			for _, sk := range res.Skipped {
				out.Skipped = append(out.Skipped, webapi.BugSkipped{Ref: sk.Proposal.ExternalRef, Card: string(sk.LocalID), Title: sk.Proposal.Title})
			}
			return out, nil
		}, nil
	})
}

func webBugProposal(p domain.BugProposal) webapi.BugProposal {
	return webapi.BugProposal{
		Ref: p.ExternalRef, Number: p.Number, Title: p.Title, OneLiner: p.OneLiner,
		Severity: string(p.Severity), State: p.State, Labels: p.Labels, Author: p.Author, Body: p.Body,
	}
}

// ImportBugs is POST /api/bugs: fetch again with the same filters, keep
// the issues the request chose, and mint them into todo — `gummi bugs
// ingest` with a checklist in place of its confirm.
func (b *Bridge) ImportBugs(ctx context.Context, req webapi.BugsRequest) (webapi.BugsCreated, error) {
	if len(req.Refs) == 0 {
		return webapi.BugsCreated{}, webErr(WebBadRequest, "choose the issues to import")
	}
	var (
		fetch    func(context.Context, engine.GitHubSource) (engine.BugIngestResult, error)
		dir      string
		eng      *engine.Engine
		opts     engine.MaterializeOpts
		perr     error
		profiles []string
	)
	if err := b.Do(ctx, func(m *Shell) tea.Cmd {
		if fetch, dir, perr = m.webBugFetch(req.In); perr != nil {
			return nil
		}
		if err := m.requireRepo(req.TargetRepo); err != nil {
			perr = webErr(WebBadRequest, "%s", err.Error())
			return nil
		}
		eng, profiles = m.engine, m.profileNames
		opts = engine.MaterializeOpts{Profile: req.Profile, Envelope: m.envelope, Repo: req.TargetRepo}
		if req.Envelope != nil {
			opts.Envelope = *req.Envelope
		}
		return nil
	}); err != nil {
		return webapi.BugsCreated{}, err
	}
	if perr != nil {
		return webapi.BugsCreated{}, perr
	}
	if opts.Profile == "" && len(profiles) > 0 {
		// the CLI's default: the first declared profile
		opts.Profile = profiles[0]
	}
	res, err := fetch(ctx, engine.GitHubSource{Repo: req.Repo, Label: req.Label, State: req.State, Dir: dir, Limit: req.Limit})
	if err != nil {
		return webapi.BugsCreated{}, webErr(WebConflict, "%s", sanitize(err.Error()))
	}
	want := map[string]bool{}
	for _, r := range req.Refs {
		want[strings.TrimSpace(r)] = true
	}
	var props []domain.BugProposal
	for _, p := range res.Proposals {
		if want[p.ExternalRef] {
			props = append(props, p)
			delete(want, p.ExternalRef)
		}
	}
	out := webapi.BugsCreated{Created: []webapi.CardRef{}}
	for _, r := range req.Refs {
		if want[strings.TrimSpace(r)] {
			out.Missing = append(out.Missing, r)
		}
	}
	if len(props) == 0 {
		return out, webErr(WebConflict, "none of those issues is on offer any more — already on the board, or filtered out")
	}
	created, merr := eng.MaterializeBugs(ctx, props, opts)
	for _, f := range created {
		out.Created = append(out.Created, webapi.CardRef{ID: string(f.ID), Title: f.Title, Stage: string(f.Stage)})
	}
	notice := noticeMsg{text: fmt.Sprintf("created %d bug%s in todo", len(created), plural(len(created))), reload: true}
	if merr != nil {
		notice = noticeMsg{text: fmt.Sprintf("bug import (created %d before failing): %s", len(created), sanitize(merr.Error())), isErr: true, reload: true}
	}
	b.deliver(notice)
	if merr != nil {
		return out, webErr(WebConflict, "%s", notice.text)
	}
	return out, nil
}
