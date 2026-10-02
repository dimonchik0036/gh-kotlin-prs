package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/cache"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/classify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

// errDemoPosts refuses every post in demo mode: its PRs are fixtures.
var errDemoPosts = fmt.Errorf("%w: demo mode never posts", actions.ErrNotPosted)

func newRunCommand(e env, global *globalOptions) *cobra.Command {
	var yes bool
	var docs strings.Builder
	for _, c := range actions.Commands {
		writef(&docs, "  %-19s %s: %s\n", c.Name, c.Text, c.Doc)
	}
	writef(&docs, "  %-19s request a review from code owners: the logins given, or by default the ones "+
		"to re-request (commented and not re-requested, or requested changes before your last push)\n", actions.RequestReview)
	names := actions.Names() + ", " + actions.RequestReview
	cmd := &cobra.Command{
		Use:   "run <number> <command> [login...]",
		Short: "Post a bot command or request a review on a PR of yours, after asking",
		Long: "Posts the command as a regular comment on the PR, after showing it and asking y/N (--yes skips " +
			"that; without a terminal --yes is required). Only on your own open PRs, and only when the command " +
			"makes sense now: no dry-run or safe-merge while one is requested or running, a safe-merge only on a " +
			"PR that isn't a draft and is approved. request-review asks the same way, then requests the review " +
			"(never from a team); only people of the PR's code-owners table can be asked, and a first assignment " +
			"needs their logins.\n\nCommands:\n" + docs.String(),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 2 || (len(args) > 2 && args[1] != actions.RequestReview) {
				return usageError{errors.New("run takes a PR number and a command, and request-review the logins to ask: " + names)}
			}
			if _, err := parseNumber(args[0]); err != nil {
				return usageError{err}
			}
			if _, ok := actions.Find(args[1]); !ok && args[1] != actions.RequestReview {
				return usageError{fmt.Errorf("unknown command %q, want one of %s", args[1], names)}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			n, _ := parseNumber(args[0])
			if args[1] == actions.RequestReview {
				return runReviewRequest(cmd.Context(), e, *global, n, args[2:], yes)
			}
			c, _ := actions.Find(args[1])
			return runCommand(cmd.Context(), e, *global, n, c, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "post without asking")
	return cmd
}

// runCommand fetches the PR, checks that the command may be posted, shows it, asks, and
// posts. The PR's cached details are dropped after a post, so the next run (the
// menu-bar plugin's, say) fetches what the bot made of it.
func runCommand(ctx context.Context, e env, global globalOptions, number int, c actions.Command, yes bool) error {
	if !yes && !e.interactive() {
		return usageError{fmt.Errorf("%w: no terminal to ask on; pass --yes to post %s", actions.ErrNotPosted, c.Text)}
	}
	cfg, client, pr, viewer, err := fetchLive(ctx, e, global, number)
	if err != nil {
		return err
	}
	if err := actions.Check(c, pr, viewer); err != nil {
		return err
	}
	rest, err := e.newREST()
	if err != nil {
		return err
	}
	writef(e.stdout, "#%d %s\n%s\n", pr.Number, pr.Title, c.Text)
	if err := e.confirm(yes, "Post this comment?"); err != nil {
		return err
	}
	url, err := github.PostComment(ctx, rest, cfg.Repo, number, c.Text)
	if err != nil {
		return err
	}
	github.ForgetPR(client, cfg.Owner(), cfg.Name(), number)
	writef(e.stdout, "posted %s\n", url)
	return nil
}

// runReviewRequest requests a review of the PR from logins, by default the ones to
// re-request (actions.PreSelected), after the same checks and question as a command.
// The PR's cached details are dropped after, so the next run sees the request.
func runReviewRequest(ctx context.Context, e env, global globalOptions, number int, logins []string, yes bool) error {
	if !yes && !e.interactive() {
		return usageError{fmt.Errorf("%w: no terminal to ask on; pass --yes to request the review", actions.ErrNotPosted)}
	}
	cfg, client, pr, viewer, err := fetchLive(ctx, e, global, number)
	if err != nil {
		return err
	}
	if err := actions.CheckOwnOpen(pr, viewer); err != nil {
		return err
	}
	subsystems := actions.Subsystems(pr)
	if len(subsystems) == 0 {
		return fmt.Errorf("%w: #%d has no code owners to ask", actions.ErrNotPosted, number)
	}
	candidates := strings.TrimSuffix(actions.CandidatesText(subsystems), "\n")
	if len(logins) == 0 {
		if logins = actions.PreSelected(pr); len(logins) == 0 {
			return fmt.Errorf("%w: nobody to re-request on #%d; name the code owners to ask, by rule:\n%s", actions.ErrNotPosted, number, candidates)
		}
	}
	if err := actions.CheckReviewRequest(pr, viewer, logins); err != nil {
		return fmt.Errorf("%w; the code owners to ask, by rule:\n%s", err, candidates)
	}
	logins = tableSpelling(subsystems, logins)
	rest, err := e.newREST()
	if err != nil {
		return err
	}
	who := strings.Join(logins, ", ")
	writef(e.stdout, "#%d %s\nrequest a review from %s\n", pr.Number, pr.Title, who)
	if err := e.confirm(yes, "Request this review?"); err != nil {
		return err
	}
	if err := github.RequestReviewers(ctx, rest, cfg.Repo, number, logins); err != nil {
		return err
	}
	github.ForgetPR(client, cfg.Owner(), cfg.Name(), number)
	writef(e.stdout, "requested a review of #%d from %s\n", number, who)
	return nil
}

// fetchLive fetches the PR live (refreshing its cache entry) and classifies it.
func fetchLive(ctx context.Context, e env, global globalOptions, number int) (config.Config, *cache.Client, model.PR, string, error) {
	cfg, _, err := prepare(e, global, "table")
	if err != nil {
		return cfg, nil, model.PR{}, "", err
	}
	client, err := e.client(global, 0)
	if err != nil {
		return cfg, nil, model.PR{}, "", err
	}
	resp, err := github.FetchPR(ctx, client, cfg.Owner(), cfg.Name(), number)
	if err != nil {
		return cfg, nil, model.PR{}, "", err
	}
	viewer := resp.Viewer.Login
	return cfg, client, (&classify.Classifier{Config: cfg, Viewer: viewer, Now: e.now()}).Show(resp.Repository.PullRequest), viewer, nil
}

// tableSpelling is the logins as the code-owners table spells them, each once.
func tableSpelling(subsystems []actions.Subsystem, logins []string) []string {
	var out []string
	for _, login := range logins {
		for _, s := range subsystems {
			if i := slices.IndexFunc(s.Candidates, func(c actions.Candidate) bool { return strings.EqualFold(c.Login, login) }); i >= 0 {
				login = s.Candidates[i].Login
				break
			}
		}
		if !slices.Contains(out, login) {
			out = append(out, login)
		}
	}
	return out
}

// confirm asks question y/N on the terminal unless yes; only y or yes goes on.
func (e env) confirm(yes bool, question string) error {
	if yes {
		return nil
	}
	writef(e.stdout, "%s [y/N] ", question)
	answer, _ := bufio.NewReader(e.stdin).ReadString('\n')
	// gh asks the terminal for its background (OSC 11, then CSI 6n) before it runs the
	// extension, and the replies may still wait on stdin: they aren't the answer.
	if a := strings.ToLower(strings.TrimSpace(ansi.Strip(answer))); a != "y" && a != "yes" {
		return fmt.Errorf("%w: cancelled", actions.ErrNotPosted)
	}
	return nil
}
