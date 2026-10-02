package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

// RESTClient is the part of go-gh's REST client the writes use; tests substitute a fake.
type RESTClient interface {
	DoWithContext(ctx context.Context, method, path string, body io.Reader, response any) error
}

// DefaultRESTClient is gh's REST client for its default host, with gh's token.
func DefaultRESTClient() (RESTClient, error) {
	return api.DefaultRESTClient()
}

// PostComment posts body as a regular comment on the PR (an issue comment, never a
// review comment) and returns the comment's URL. With RequestReviewers, it's all the
// tool ever writes.
func PostComment(ctx context.Context, client RESTClient, repo string, number int, body string) (string, error) {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return "", err
	}
	var resp struct {
		HTMLURL string `json:"html_url"`
	}
	path := fmt.Sprintf("repos/%s/issues/%d/comments", repo, number)
	if err := client.DoWithContext(ctx, http.MethodPost, path, bytes.NewReader(payload), &resp); err != nil {
		return "", fmt.Errorf("post %s to #%d: %w", body, number, err)
	}
	return resp.HTMLURL, nil
}

// RequestReviewers requests a review of the PR from the people, all in one request:
// GitHub adds them to the requested reviewers, keeping the ones already there, and
// re-requests those who reviewed before. A 422 (someone can't be requested) requests
// nobody. It never requests teams.
func RequestReviewers(ctx context.Context, client RESTClient, repo string, number int, logins []string) error {
	payload, err := json.Marshal(map[string][]string{"reviewers": logins})
	if err != nil {
		return err
	}
	path := fmt.Sprintf("repos/%s/pulls/%d/requested_reviewers", repo, number)
	if err := client.DoWithContext(ctx, http.MethodPost, path, bytes.NewReader(payload), nil); err != nil {
		return fmt.Errorf("request a review of #%d from %s: %w", number, strings.Join(logins, ", "), err)
	}
	return nil
}
