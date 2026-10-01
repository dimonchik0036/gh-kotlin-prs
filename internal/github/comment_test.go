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
