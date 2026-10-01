package github

import (
	"fmt"
	"regexp"
)

// GitHub web URLs, built and recognized in one place.

const web = "https://github.com/"

// ProfileURL is a user's profile page.
func ProfileURL(login string) string { return web + login }

// TeamURL is a team's page in an organization.
func TeamURL(org, slug string) string { return web + "orgs/" + org + "/teams/" + slug }

// PRURL is a pull request's page; repo is owner/name.
func PRURL(repo string, number int) string { return fmt.Sprintf("%s%s/pull/%d", web, repo, number) }

var teamURL = regexp.MustCompile(`^https://github\.com/orgs/([^/?#]+)/teams/([^/?#]+)/?$`)

// ParseTeamURL recognizes a team page of any organization.
func ParseTeamURL(url string) (org, slug string, ok bool) {
	m := teamURL.FindStringSubmatch(url)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
