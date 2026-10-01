package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/actions"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/classify"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
)

// errDemoPosts refuses every post in demo mode: its PRs are fixtures.
var errDemoPosts = fmt.Errorf("%w: demo mode never posts", actions.ErrNotPosted)

func newRunCommand(e env, global *globalOptions) *cobra.Command {
	var yes bool
	var docs strings.Builder
	for _, c := range actions.Commands {
		writef(&docs, "  %-19s %s: %s\n", c.Name, c.Text, c.Doc)
	}
	cmd := &cobra.Command{
		Use:   "run <number> <command>",
		Short: "Post a bot command on a PR of yours, after asking",
		Long: "Posts the command as a regular comment on the PR, after showing it and asking y/N (--yes skips " +
			"that; without a terminal --yes is required). Only on your own open PRs, and only when the command " +
			"makes sense now: no dry-run or safe-merge while one is requested or running, a safe-merge only on a " +
			"PR that isn't a draft and is approved.\n\nCommands:\n" + docs.String(),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usageError{errors.New("run takes a PR number and a command: " + actions.Names())}
			}
			if _, err := parseNumber(args[0]); err != nil {
				return usageError{err}
			}
			if _, ok := actions.Find(args[1]); !ok {
				return usageError{fmt.Errorf("unknown command %q, want one of %s", args[1], actions.Names())}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			n, _ := parseNumber(args[0])
			c, _ := actions.Find(args[1])
			return runCommand(cmd.Context(), e, *global, n, c, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "post without asking")
	return cmd
}

// runCommand fetches the PR, checks that the command may be posted, shows it, asks, and posts.
func runCommand(ctx context.Context, e env, global globalOptions, number int, c actions.Command, yes bool) error {
	if !yes && !e.interactive() {
		return usageError{fmt.Errorf("%w: no terminal to ask on; pass --yes to post %s", actions.ErrNotPosted, c.Text)}
	}
	cfg, _, err := prepare(e, global, "table")
	if err != nil {
		return err
	}
	client, err := e.client(global, 0)
	if err != nil {
		return err
	}
	resp, err := github.FetchPR(ctx, client, cfg.Owner(), cfg.Name(), number)
	if err != nil {
		return err
	}
	viewer := resp.Viewer.Login
	pr := (&classify.Classifier{Config: cfg, Viewer: viewer, Now: e.now()}).Show(resp.Repository.PullRequest)
	if err := actions.Check(c, pr, viewer); err != nil {
		return err
	}
	rest, err := e.newREST()
	if err != nil {
		return err
	}
	writef(e.stdout, "#%d %s\n%s\n", pr.Number, pr.Title, c.Text)
	if !yes {
		writef(e.stdout, "Post this comment? [y/N] ")
		answer, _ := bufio.NewReader(e.stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return fmt.Errorf("%w: cancelled", actions.ErrNotPosted)
		}
	}
	url, err := github.PostComment(ctx, rest, cfg.Repo, number, c.Text)
	if err != nil {
		return err
	}
	writef(e.stdout, "posted %s\n", url)
	return nil
}
