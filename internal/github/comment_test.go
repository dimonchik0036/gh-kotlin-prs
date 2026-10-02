package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

// toServer sends every request to the test server instead of GitHub.
type toServer struct{ server *url.URL }

func (t toServer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = t.server.Scheme, t.server.Host
	return http.DefaultTransport.RoundTrip(req)
}

// PostComment posts an issue comment through gh's REST client: the request it sends.
func TestPostComment(t *testing.T) {
	var method, path, auth string
	var body map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id": 1, "html_url": "https://github.com/JetBrains/kotlin/pull/90006#issuecomment-1"}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client, err := api.NewRESTClient(api.ClientOptions{Host: "github.com", AuthToken: "test-token", Transport: toServer{u}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := PostComment(context.Background(), client, "JetBrains/kotlin", 90006, "/dry-run --retry")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://github.com/JetBrains/kotlin/pull/90006#issuecomment-1" {
		t.Errorf("URL %q", got)
	}
	if method != http.MethodPost || path != "/repos/JetBrains/kotlin/issues/90006/comments" || len(body) != 1 || body["body"] != "/dry-run --retry" {
		t.Errorf("%s %s %v", method, path, body)
	}
	if !strings.HasSuffix(auth, "test-token") {
		t.Errorf("Authorization %q", auth)
	}
}

func TestPostCommentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message": "Resource not accessible by integration"}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client, _ := api.NewRESTClient(api.ClientOptions{Host: "github.com", AuthToken: "test-token", Transport: toServer{u}})
	if _, err := PostComment(context.Background(), client, "JetBrains/kotlin", 7, "/fixup"); err == nil ||
		!strings.Contains(err.Error(), "post /fixup to #7") || !strings.Contains(err.Error(), "403") {
		t.Errorf("error %v", err)
	}
}

// RequestReviewers asks for every login in one request, and never for a team.
func TestRequestReviewers(t *testing.T) {
	var method, path string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"number": 90010, "html_url": "https://github.com/JetBrains/kotlin/pull/90010"}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client, err := api.NewRESTClient(api.ClientOptions{Host: "github.com", AuthToken: "test-token", Transport: toServer{u}})
	if err != nil {
		t.Fatal(err)
	}
	if err := RequestReviewers(context.Background(), client, "JetBrains/kotlin", 90010, []string{"bob_user", "carol_user"}); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/repos/JetBrains/kotlin/pulls/90010/requested_reviewers" {
		t.Errorf("%s %s", method, path)
	}
	if got, _ := json.Marshal(body); string(got) != `{"reviewers":["bob_user","carol_user"]}` {
		t.Errorf("body %s", got)
	}
}

// A 422 requests nobody: the error names the PR and the people.
func TestRequestReviewersError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message": "Reviews may only be requested from collaborators."}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client, _ := api.NewRESTClient(api.ClientOptions{Host: "github.com", AuthToken: "test-token", Transport: toServer{u}})
	err := RequestReviewers(context.Background(), client, "JetBrains/kotlin", 7, []string{"bob_user"})
	if err == nil || !strings.Contains(err.Error(), "request a review of #7 from bob_user") || !strings.Contains(err.Error(), "422") {
		t.Errorf("error %v", err)
	}
}
