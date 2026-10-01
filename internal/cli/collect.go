package cli

import (
	"context"
	"time"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/github"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/listing"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/model"
)

type listOptions struct {
	mine, review bool
	waitingOnMe  bool
	all          bool
	noTeams      bool
	noMerged     bool
	format       string
	maxAge       time.Duration
}

// sections returns the sections to show, in display order.
func (o listOptions) sections() []model.Section {
	both := o.mine == o.review
	var out []model.Section
	if both || o.mine {
		out = append(out, model.SectionMine)
	}
	if both || o.review {
		out = append(out, model.SectionReview)
		if !o.noTeams {
			out = append(out, model.SectionTeams)
		}
	}
	if (both || o.mine) && !o.noMerged {
		out = append(out, model.SectionMerged)
	}
	return out
}

// shown is what `list` shows.
type shown struct {
	viewer string
	prs    []model.PR
	// hidden counts the rows of each section that only --all shows.
	hidden map[model.Section]model.Hidden
}

// collect fetches the PRs of the requested sections, classifies them and applies --all
// and --waiting-on-me.
func collect(ctx context.Context, client github.Client, cfg config.Config, now time.Time, opts listOptions) (*shown, error) {
	data, err := listing.Fetch(ctx, client, cfg, now, opts.sections())
	if err != nil {
		return nil, err
	}
	prs, hidden := listing.Filter(data.Classify(cfg, now), opts.all, opts.waitingOnMe)
	return &shown{viewer: data.Viewer, prs: prs, hidden: hidden}, nil
}
