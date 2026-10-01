package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cli/go-gh/v2/pkg/api"
)

// RESTClient is the part of go-gh's REST client used to post comments; tests substitute a fake.
type RESTClient interface {
	DoWithContext(ctx context.Context, method, path string, body io.Reader, response any) error
}

// DefaultRESTClient is gh's REST client for its default host, with gh's token.
func DefaultRESTClient() (RESTClient, error) {
	return api.DefaultRESTClient()
}

// PostComment posts body as a regular comment on the PR (an issue comment, never a
// review comment) and returns the comment's URL. It's the only write of the tool.
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
