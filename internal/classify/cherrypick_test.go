package classify

import (
	"slices"
	"strconv"
	"testing"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

func TestParseCherryPick(t *testing.T) {
	for _, tt := range []struct {
		body   string
		number int
		author string
	}{
		{"Original pull request: https://github.com/JetBrains/kotlin/pull/5 by @alice_user\n", 5, "alice_user"},
		{"Original pull request: https://github.com/JetBrains/kotlin/pull/42 by @bob-user  \r\n\nEdited below.", 42, "bob-user"},
		{"Original pull request: https://github.com/JetBrains/kotlin/pull/5 by @KotlinBuild", 5, "KotlinBuild"},
		{"Original pull request: https://github.com/JetBrains/kotlin/pull/5", 0, ""},
		{"Original pull request: https://github.com/JetBrains/kotlin/issues/5 by @alice_user", 0, ""},
		{"Original pull request: https://github.com/JetBrains/kotlin/pull/5 by @alice_user and @bob_user", 0, ""},
		{"See https://github.com/JetBrains/kotlin/pull/5 by @alice_user", 0, ""},
		{"", 0, ""},
	} {
		original, author, ok := ParseCherryPick(tt.body)
		if ok != (tt.number != 0) || original.Number != tt.number || author != tt.author {
			t.Errorf("ParseCherryPick(%q) = %+v, %q, %v", tt.body, original, author, ok)
		}
		if ok && original.URL != "https://github.com/JetBrains/kotlin/pull/"+strconv.Itoa(tt.number) {
			t.Errorf("ParseCherryPick(%q) URL = %q", tt.body, original.URL)
		}
	}
}

// A bot's PR is its assignees', or without any, a cherry-pick's original author's; a
// person's PR is its author's, whoever is assigned.
func TestOwners(t *testing.T) {
	cfg := testClassifier().Config
	agent := func() *prBuilder { b := newPR("agent"); b.pr.Author = botActor("agent"); return b }
	for _, tt := range []struct {
		name     string
		pr       *prBuilder
		owners   []string
		original string // the original author, "" for no original PR
	}{
		{"the bot's cherry-pick", cherryPick(me), []string{me}, me},
		{"someone else's", cherryPick("alice_user"), []string{"alice_user"}, "alice_user"},
		{"assigned to whoever asked for it", cherryPick(me).assign("bob_user"), []string{"bob_user"}, me},
		{"assigned to two", cherryPick(me).assign("bob_user", me), []string{"bob_user", me}, me},
		{"a bot assignee doesn't answer for it", cherryPick(me).assign("kodee-bot"), []string{me}, me},
		{"a cherry-pick of a cherry-pick", cherryPick("KotlinBuild"), []string{"KotlinBuild"}, "KotlinBuild"},
		{"assigned, a cherry-pick of a cherry-pick", cherryPick("KotlinBuild").assign("bob_user"), []string{"bob_user"}, "KotlinBuild"},
		{"the line written by a person", func() *prBuilder { b := newPR("bob_user"); b.pr.Body = cherryPick(me).pr.Body; return b }(), []string{"bob_user"}, ""},
		{"the bot's PR without the line", newPR("KotlinBuild"), []string{"KotlinBuild"}, ""},
		{"another bot's PR assigned to me", agent().assign(me), []string{me}, ""},
		{"a person's PR assigned to me", newPR("bob_user").assign(me), []string{"bob_user"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			owners, original := Owners(cfg, tt.pr.build())
			if !slices.Equal(owners, tt.owners) || (original == nil) != (tt.original == "") || original != nil && (original.Author != tt.original || original.Number != 5) {
				t.Errorf("Owners = %q, %+v; want %q, by %q", owners, original, tt.owners, tt.original)
			}
		})
	}
	cfg.CherryPickBot = ""
	if owners, original := Owners(cfg, cherryPick(me).build()); !slices.Equal(owners, []string{"KotlinBuild"}) || original != nil {
		t.Errorf("without a cherry-pick bot: %q, %+v", owners, original)
	}
}

// My cherry-pick is mine everywhere: its section, its rules, its author.
func TestCherryPickIsMine(t *testing.T) {
	c := testClassifier()
	raw := cherryPick(me).ownersGreen().reviewed("alice_user", "APPROVED", at(30)).
		status(aggregate, "SUCCESS", at(60)).build()
	if s := c.SectionFor(raw); s != model.SectionMine {
		t.Errorf("section = %s", s)
	}
	pr := c.Show(raw)
	if pr.Author != me || pr.CherryPickOf == nil || pr.CherryPickOf.Number != 5 ||
		pr.CherryPickOf.URL != "https://github.com/JetBrains/kotlin/pull/5" || !pr.Release {
		t.Errorf("pr = %+v", pr)
	}
	if pr.Next != model.NextRelease || pr.Primary() != "approved, waiting for the release engineer to merge" {
		t.Errorf("next %s, %q", pr.Next, pr.Texts())
	}
	// My own review there isn't a reviewer's.
	reviewed := c.PR(cherryPick(me).ownersGreen().reviewed(me, "APPROVED", at(30)).status(aggregate, "SUCCESS", at(60)).build(), model.SectionMine)
	if reviewed.Approvals != 0 || reviewed.Next == model.NextRelease {
		t.Errorf("my approval counted: %d approvals, next %s", reviewed.Approvals, reviewed.Next)
	}
	// Someone else's is a review of mine when requested.
	theirs := cherryPick("alice_user").request(me).build()
	if s := c.SectionFor(theirs); s != model.SectionReview {
		t.Errorf("someone else's cherry-pick: section %s", s)
	}
	if merged := c.Merged(cherryPick(me).build()); merged.Author != me || merged.CherryPickOf == nil {
		t.Errorf("merged = %+v", merged)
	}
	// Assigned to whoever asked for it, it's theirs, the original author's no more.
	handed := c.Show(cherryPick(me).assign("bob_user").build())
	if handed.Section == model.SectionMine || handed.Author != "bob_user" || handed.CherryPickOf.Author != me {
		t.Errorf("assigned to someone else: section %s, author %q, original %+v", handed.Section, handed.Author, handed.CherryPickOf)
	}
	shared := c.Show(cherryPick("alice_user").assign("bob_user", me).build())
	if shared.Section != model.SectionMine || shared.Author != me || !slices.Equal(shared.Assignees, []string{"bob_user", me}) {
		t.Errorf("assigned to me too: section %s, author %q, assignees %q", shared.Section, shared.Author, shared.Assignees)
	}
}
