package actions

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// firstAssignment is a PR of mine whose table has rows nobody was asked for, like a
// freshly opened PR touching four subsystems.
func firstAssignment() model.PR {
	pr := ready()
	pr.CodeOwners = model.CodeOwnersStatus{State: model.CodeOwnersMissing, Check: "FAILURE", Rules: []model.CodeOwnerRule{
		{Paths: []string{"/analysis/"}, Teams: []string{"kotlin-analysis-api"}, Mark: model.MarkNoReview, Owners: []model.Owner{
			{Login: "bob_user", Team: "kotlin-analysis-api"}, {Login: "alice_user", Team: "kotlin-analysis-api"},
			{Login: "dave_user", Team: "kotlin-analysis-api"}}},
		{Paths: []string{"/compiler/fir/", "/compiler/frontend.common.jvm/", "/core/descriptors.jvm/", "/core/deserialization.common.jvm/"},
			Teams: []string{"kotlin-frontend"}, Mark: model.MarkApproved, Assignees: []model.Assignee{{Login: "frank_user", Final: true}},
			Owners: []model.Owner{{Login: "erin_user"}, {Login: "frank_user"}, {Login: "grace_user", Unavailable: true}}},
		{Paths: []string{"/compiler/testData/codegen/asmLike/"}, Teams: []string{"kotlin-jvm"}, Mark: model.MarkNoReview, Owners: []model.Owner{
			{Login: "judy_user"}, {Login: "kim_user", Unavailable: true}, {Login: "mia_user", Role: "QA"}, {Login: "leo_user"}}},
		{Paths: []string{"/core/descriptors.runtime/"}, Mark: model.MarkNoReview, Owners: []model.Owner{
			{Login: "nora_user"}, {Login: "judy_user"}, {Login: "oscar_user", Unavailable: true}}},
		{Paths: []string{"/plugins/parcelize/"}, Mark: model.MarkNoReview}, // #NO_OWNERS
		{Paths: []string{"/libraries/"}, Mark: model.MarkNoReview, Owners: []model.Owner{{Login: "alice_user"}}},
	}}
	pr.Reviewers = []model.Reviewer{
		{Login: "dave_user", State: model.ReviewerCommented},
		{Login: "frank_user", State: model.ReviewerApproved, Final: true, CodeOwner: true},
	}
	return pr
}

func logins(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Login
	}
	return out
}

func TestSubsystems(t *testing.T) {
	got := Subsystems(firstAssignment())
	type row struct {
		path       string
		more       int
		candidates []string
		status     string
		needsMe    bool
		covered    bool
	}
	want := []row{
		{"/analysis/", 0, []string{"bob_user", "dave_user"}, "unassigned", true, false},
		{"/compiler/fir/", 3, []string{"erin_user", "frank_user", "grace_user"}, "✓ frank_user", false, true},
		// QA after the others, ⏳ last, whatever the table's order.
		{"/compiler/testData/codegen/asmLike/", 0, []string{"judy_user", "leo_user", "mia_user", "kim_user"}, "unassigned", true, false},
		{"/core/descriptors.runtime/", 0, []string{"nora_user", "judy_user", "oscar_user"}, "unassigned", true, false},
	}
	if len(got) != len(want) {
		t.Fatalf("%d subsystems, want %d (no #NO_OWNERS row, none only the author owns): %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := row{got[i].Path, got[i].More, logins(got[i].Candidates), got[i].Status, got[i].NeedsMe, got[i].Covered}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("row %d: %+v, want %+v", i, g, w)
		}
	}
	hints := map[string]string{}
	for _, s := range got {
		for _, c := range s.Candidates {
			hints[s.Path+" "+c.Login] = c.Hint()
		}
	}
	for key, want := range map[string]string{
		"/analysis/ dave_user":                          "commented",
		"/analysis/ bob_user":                           "",
		"/compiler/fir/ frank_user":                     "approved",
		"/compiler/testData/codegen/asmLike/ judy_user": "also /core/descriptors.runtime/",
		"/core/descriptors.runtime/ judy_user":          "also /compiler/testData/codegen/asmLike/",
		"/compiler/testData/codegen/asmLike/ mia_user":  "",
		"/core/descriptors.runtime/ oscar_user":         "",
	} {
		if hints[key] != want {
			t.Errorf("%s: hint %q, want %q", key, hints[key], want)
		}
	}
	if s := got[2].Candidates; s[2].Role != "QA" || !s[3].Unavailable {
		t.Errorf("roles and ⏳ lost: %+v", s)
	}
}

// The table lags behind a request: an owner requested now covers an UNASSIGNED row.
func TestSubsystemsStaleTable(t *testing.T) {
	pr := firstAssignment()
	pr.Reviewers = append(pr.Reviewers, model.Reviewer{Login: "bob_user", State: model.ReviewerPending, Requested: true})
	s := Subsystems(pr)[0]
	if s.Status != "requested" || s.NeedsMe || !s.Covered || s.Candidates[0].Hint() != "requested" {
		t.Errorf("%+v", s)
	}
}

// reRequest is a PR of mine after a round of review: dave_user commented on a rule (🔄),
// peggy_user requested changes before my last push, frank_user approved.
func reRequest() model.PR {
	pr := firstAssignment()
	pr.LastPush = time.Unix(2000, 0)
	pr.CodeOwners.Rules[0].Mark, pr.CodeOwners.Rules[0].Assignees = model.MarkReRequest, []model.Assignee{{Login: "dave_user"}}
	pr.CodeOwners.Rules[2].Mark, pr.CodeOwners.Rules[2].Assignees = model.MarkChangesRequested, []model.Assignee{{Login: "leo_user"}}
	pr.CodeOwners.Rules[3].Owners = append(pr.CodeOwners.Rules[3].Owners, model.Owner{Login: "peggy_user"})
	pr.CodeOwners.Rules[3].Mark, pr.CodeOwners.Rules[3].Assignees = model.MarkChangesRequested, []model.Assignee{{Login: "peggy_user"}}
	pr.Reviewers = []model.Reviewer{
		{Login: "dave_user", State: model.ReviewerCommented, ReRequest: true, CodeOwner: true},
		{Login: "frank_user", State: model.ReviewerApproved, Final: true, CodeOwner: true},
		{Login: "peggy_user", State: model.ReviewerChangesRequested, At: time.Unix(1000, 0), CodeOwner: true},
		// Changes requested after my push: my move, not a re-request.
		{Login: "leo_user", State: model.ReviewerChangesRequested, At: time.Unix(3000, 0), CodeOwner: true},
		// Changes requested by someone outside the table: not a candidate.
		{Login: "quinn_user", State: model.ReviewerChangesRequested, At: time.Unix(1000, 0)},
	}
	return pr
}

func TestPreSelected(t *testing.T) {
	if got := PreSelected(reRequest()); !reflect.DeepEqual(got, []string{"dave_user", "peggy_user"}) {
		t.Errorf("re-request: %q", got)
	}
	if got := PreSelected(firstAssignment()); len(got) != 0 {
		t.Errorf("first assignment: %q, want nobody", got)
	}
	requested := reRequest()
	requested.Reviewers[0].Requested = true
	if got := PreSelected(requested); !reflect.DeepEqual(got, []string{"peggy_user"}) {
		t.Errorf("dave_user re-requested already: %q", got)
	}
	// A 🔄 assignee who approved since (the table lags) isn't asked again.
	approved := reRequest()
	approved.Reviewers[0].State = model.ReviewerApproved
	if got := PreSelected(approved); !reflect.DeepEqual(got, []string{"peggy_user"}) {
		t.Errorf("dave_user approved: %q", got)
	}
	s := Subsystems(reRequest())
	if s[0].Status != "re-request: dave_user" || !s[0].NeedsMe || s[2].Status != "changes requested: leo_user" || s[2].NeedsMe {
		t.Errorf("statuses %q %v, %q %v", s[0].Status, s[0].NeedsMe, s[2].Status, s[2].NeedsMe)
	}
}

func TestCheckReviewRequest(t *testing.T) {
	tests := []struct {
		name   string
		edit   func(pr *model.PR)
		logins []string
		reason string
	}{
		{"one candidate", func(*model.PR) {}, []string{"bob_user"}, ""},
		{"candidates of several rows, any case", func(*model.PR) {}, []string{"Judy_User", "nora_user"}, ""},
		{"a draft", func(pr *model.PR) { pr.Draft = true }, []string{"bob_user"}, ""},
		{"not mine", func(pr *model.PR) { pr.Author = "bob_user" }, []string{"dave_user"}, "#7 isn't yours (by bob_user)"},
		{"merged", func(pr *model.PR) { pr.MergedAt = time.Unix(1, 0) }, []string{"bob_user"}, "#7 is merged"},
		{"closed", func(pr *model.PR) { pr.Closed = true }, []string{"bob_user"}, "#7 is closed"},
		{"nobody", func(*model.PR) {}, nil, "nobody picked to request a review of #7 from"},
		{"not in the table", func(*model.PR) {}, []string{"bob_user", "zed_user"}, "zed_user is no code owner of #7 to ask"},
		{"myself", func(*model.PR) {}, []string{"alice_user", "zed_user"}, "alice_user, zed_user are no code owner of #7 to ask"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := firstAssignment()
			tt.edit(&pr)
			err := CheckReviewRequest(pr, me, tt.logins)
			switch {
			case tt.reason == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tt.reason != "" && (err == nil || !errors.Is(err, ErrNotPosted) || !strings.HasSuffix(err.Error(), tt.reason)):
				t.Errorf("error %v, want %q", err, tt.reason)
			}
		})
	}
}

func TestCandidatesText(t *testing.T) {
	want := "  /analysis/ (unassigned): bob_user, dave_user\n" +
		"  /compiler/fir/ +3 (✓ frank_user): erin_user, frank_user, grace_user\n" +
		"  /compiler/testData/codegen/asmLike/ (unassigned): judy_user, leo_user, mia_user, kim_user\n" +
		"  /core/descriptors.runtime/ (unassigned): nora_user, judy_user, oscar_user\n"
	if got := CandidatesText(Subsystems(firstAssignment())); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
