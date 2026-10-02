package classify

import (
	"fmt"
	"strings"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// A small builder for synthetic PRs in rule tests. Bot comments use the real templates.

const me = "dimonchik0036"

// t0 is the last push of a fresh PR; the clock is at t0+2h.
var t0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }

var testNow = at(120)

func testClassifier() *Classifier {
	return &Classifier{Config: config.Default(), Viewer: me, Now: testNow}
}

type prBuilder struct{ pr github.PullRequest }

func newPR(author string) *prBuilder {
	b := &prBuilder{pr: github.PullRequest{
		Number:      1,
		Title:       "KT-1: Test",
		URL:         "https://github.com/JetBrains/kotlin/pull/1",
		State:       "OPEN",
		Author:      user(author),
		HeadRefName: author + "/KT-1.test",
		UpdatedAt:   t0,
	}}
	b.pr.Commits.Nodes = []github.PullRequestCommit{{Commit: github.Commit{CommittedDate: t0}}}
	return b
}

func user(login string) *github.Actor { return &github.Actor{Typename: "User", Login: login} }

func botActor(login string) *github.Actor { return &github.Actor{Typename: "Bot", Login: login} }

func (b *prBuilder) build() *github.PullRequest { return &b.pr }

// commitMessage adds a commit to the history, oldest first.
func (b *prBuilder) commitMessage(msg string) *prBuilder {
	b.pr.History.Nodes = append(b.pr.History.Nodes, github.PullRequestCommit{Commit: github.Commit{Message: msg}})
	return b
}

func (b *prBuilder) mine() model.PR { return testClassifier().PR(b.build(), model.SectionMine) }

func (b *prBuilder) draft() *prBuilder {
	b.pr.IsDraft = true
	return b
}

func (b *prBuilder) forcePush(t time.Time) *prBuilder {
	b.pr.TimelineItems.Nodes = append(b.pr.TimelineItems.Nodes, github.TimelineItem{Typename: "HeadRefForcePushedEvent", CreatedAt: t})
	return b
}

type commentOpt func(*github.Comment)

func rocket(c *github.Comment) {
	c.Reactions.Nodes = append(c.Reactions.Nodes, github.Reaction{User: &github.Actor{Login: "kotlin-safemerge[bot]"}})
}

func minimized(c *github.Comment) { c.IsMinimized = true }

func association(a string) commentOpt { return func(c *github.Comment) { c.AuthorAssociation = a } }

func edited(t time.Time) commentOpt { return func(c *github.Comment) { c.UpdatedAt = t } }

func (b *prBuilder) comment(author *github.Actor, body string, t time.Time, opts ...commentOpt) *prBuilder {
	c := github.Comment{Author: author, Body: body, CreatedAt: t, UpdatedAt: t,
		URL: fmt.Sprintf("%s#issuecomment-%d", b.pr.URL, 100+len(b.pr.Comments.Nodes))}
	for _, o := range opts {
		o(&c)
	}
	b.pr.Comments.Nodes = append(b.pr.Comments.Nodes, c)
	return b
}

func (b *prBuilder) says(login, body string, t time.Time, opts ...commentOpt) *prBuilder {
	return b.comment(user(login), body, t, opts...)
}

// gate posts a KotlinBuild comment; result is "", "passed" or "failed".
func (b *prBuilder) gate(kind model.RunKind, build int, result string, t time.Time, opts ...commentOpt) *prBuilder {
	return b.comment(user("KotlinBuild"), gateBody(kind, build, result), t, opts...)
}

func gateBody(kind model.RunKind, build int, result string) string {
	var s strings.Builder
	if kind == model.DryRun {
		s.WriteString("**THIS IS A DRY RUN**\n\n")
	}
	writef(&s, "Quality gate is triggered at https://buildserver.labs.intellij.net/build/%d — use this link to get full insight.\n\n", build)
	s.WriteString("Quality gate was triggered with the following revisions:\n> **kotlin**\n> Branch: `refs/merge/GITHUB-1/safe-merge`\n> Commit: [0123456](https://github.com/JetBrains/kotlin/commit/0123456789abcdef0123456789abcdef01234567)\n\n\n")
	switch result {
	case "passed":
		s.WriteString("\n---\n\nQuality gate finished successfully.")
	case "failed":
		writef(&s, "\n---\n\nQuality gate failed. See https://buildserver.labs.intellij.net/build/%d to get full insight.", build)
	case "merge failed":
		// The gate passed, the merge didn't.
		s.WriteString("\n---\n\nQuality gate finished successfully.\n\n---\n\n" + mergeError)
	}
	return s.String()
}

const mergeError = "Error: Failed to merge PR #1: rebase-merge failed: Pull Request has merge conflicts"

func (b *prBuilder) rejected(reason string, t time.Time, opts ...commentOpt) *prBuilder {
	return b.botReply("Command rejected: "+reason, t, opts...)
}

// botReply posts any reply of the code-owners bot.
func (b *prBuilder) botReply(text string, t time.Time, opts ...commentOpt) *prBuilder {
	return b.comment(botActor("kotlin-safemerge"), text, t, opts...)
}

func (b *prBuilder) request(logins ...string) *prBuilder {
	for _, l := range logins {
		b.pr.ReviewRequests.Nodes = append(b.pr.ReviewRequests.Nodes, github.ReviewRequest{RequestedReviewer: &github.Reviewer{Typename: "User", Login: l}})
	}
	return b
}

func (b *prBuilder) requestTeam(slug string) *prBuilder {
	b.pr.ReviewRequests.Nodes = append(b.pr.ReviewRequests.Nodes, github.ReviewRequest{RequestedReviewer: &github.Reviewer{Typename: "Team", Slug: slug}})
	return b
}

func (b *prBuilder) requestEvent(login string, t time.Time) *prBuilder {
	b.pr.TimelineItems.Nodes = append(b.pr.TimelineItems.Nodes, github.TimelineItem{
		Typename: "ReviewRequestedEvent", CreatedAt: t, RequestedReviewer: &github.Reviewer{Typename: "User", Login: login},
	})
	return b
}

// reviewed submits a review; APPROVED and CHANGES_REQUESTED also become the reviewer's latest opinion.
func (b *prBuilder) reviewed(login, state string, t time.Time) *prBuilder {
	return b.reviewedWith(login, state, t, 0)
}

// reviewedWith submits a review with that many thread comments.
func (b *prBuilder) reviewedWith(login, state string, t time.Time, comments int) *prBuilder {
	r := github.Review{Author: user(login), State: state, SubmittedAt: t, Comments: &github.Count{TotalCount: comments},
		URL: fmt.Sprintf("%s#pullrequestreview-%d", b.pr.URL, 200+len(b.pr.Reviews.Nodes))}
	b.pr.Reviews.Nodes = append(b.pr.Reviews.Nodes, r)
	if state != "COMMENTED" {
		ops := b.pr.LatestOpinionatedReviews.Nodes[:0]
		for _, op := range b.pr.LatestOpinionatedReviews.Nodes {
			if op.Author.Login != login {
				ops = append(ops, op)
			}
		}
		b.pr.LatestOpinionatedReviews.Nodes = append(ops, r)
	}
	return b
}

type threadComment struct {
	login string
	at    time.Time
}

func by(login string, t time.Time) threadComment { return threadComment{login, t} }

func (b *prBuilder) thread(resolved, outdated bool, comments ...threadComment) *prBuilder {
	th := github.ReviewThread{IsResolved: resolved, IsOutdated: outdated, Path: "compiler/A.kt"}
	for i, c := range comments {
		tc := github.ThreadComment{Author: user(c.login), CreatedAt: c.at,
			URL: fmt.Sprintf("%s#discussion_r%d", b.pr.URL, 300+10*len(b.pr.ReviewThreads.Nodes)+i)}
		if i == 0 {
			first := tc
			first.Body = "Why?"
			th.FirstComment.Nodes = []github.ThreadComment{first}
		}
		th.Comments.Nodes = append(th.Comments.Nodes, tc)
	}
	th.Comments.TotalCount = len(comments)
	b.pr.ReviewThreads.Nodes = append(b.pr.ReviewThreads.Nodes, th)
	return b
}

func (b *prBuilder) check(name, conclusion string) *prBuilder {
	c := &b.pr.Commits.Nodes[0].Commit
	if c.StatusCheckRollup == nil {
		c.StatusCheckRollup = &github.StatusCheckRollup{}
	}
	c.StatusCheckRollup.Contexts.Nodes = append(c.StatusCheckRollup.Contexts.Nodes, github.CheckContext{Typename: "CheckRun", Name: name, Conclusion: conclusion, Status: "COMPLETED"})
	return b
}

func (b *prBuilder) ownersGreen() *prBuilder { return b.check(codeOwnersCheck, "SUCCESS") }

func (b *prBuilder) ownersRed() *prBuilder { return b.check(codeOwnersCheck, "FAILURE") }

// ownerRow is one row of the code-owners table, in the bot's HTML.
type ownerRow struct {
	path      string
	team      string
	members   []string // "login", "login ⏳", "login (QA)"
	mark      string
	assignees []string // "login", "login 🔒"; none means UNASSIGNED
}

// owners posts the code-owners comment.
func (b *prBuilder) owners(rows ...ownerRow) *prBuilder {
	return b.comment(botActor("kotlin-safemerge"), ownersTableHTML(rows), t0.Add(-time.Hour))
}

// ownersCheck reports the code-owners check with the table in its summary, as the bot does.
func (b *prBuilder) ownersCheck(conclusion string, rows ...ownerRow) *prBuilder {
	b.check(codeOwnersCheck, conclusion)
	contexts := &b.pr.Commits.Nodes[0].Commit.StatusCheckRollup.Contexts
	contexts.Nodes[len(contexts.Nodes)-1].Summary = ownersTableHTML(rows)
	return b
}

func ownersTableHTML(rows []ownerRow) string {
	var s strings.Builder
	s.WriteString("### Code Owners\n\n<table><tr><th>Rule</th><th>Owners</th><th>Approval</th></tr>")
	userLink := func(login string) string {
		return fmt.Sprintf(`<a href="https://github.com/%s"><b><code>%s</code></b></a>`, login, login)
	}
	for _, r := range rows {
		writef(&s, "<tr><td><code>%s</code></td><td>", strings.ReplaceAll(r.path, "/", "/\u200b"))
		if r.team != "" {
			writef(&s, `<details><summary><a href="https://github.com/orgs/JetBrains/teams/%s">%s</a></summary><ul>`, r.team, r.team)
			for _, m := range r.members {
				login, suffix, _ := strings.Cut(m, " ")
				if suffix != "" {
					suffix = " " + suffix
				}
				writef(&s, "<li>%s%s</li>", userLink(login), suffix)
			}
			s.WriteString("</ul></details>")
		}
		writef(&s, `</td><td align="center">%s<br>`, r.mark)
		if len(r.assignees) == 0 {
			s.WriteString("<b><code>UNASSIGNED</code></b>")
		}
		for i, a := range r.assignees {
			if i > 0 {
				s.WriteString(", ")
			}
			login, suffix, _ := strings.Cut(a, " ")
			s.WriteString(userLink(login))
			if suffix != "" {
				s.WriteString(" " + suffix)
			}
		}
		s.WriteString("</td></tr>")
	}
	s.WriteString("</table>\n\n<!-- CODE_OWNERS_REVIEW_COMMENT -->")
	return s.String()
}

// writef is fmt.Fprintf into a strings.Builder, whose writes never fail.
func writef(b *strings.Builder, format string, args ...any) {
	_, _ = fmt.Fprintf(b, format, args...)
}
