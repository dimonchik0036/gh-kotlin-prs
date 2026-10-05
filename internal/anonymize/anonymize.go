// Package anonymize rewrites raw GraphQL responses before they are committed as test
// fixtures. Human logins become stable pseudonyms (alice_user, bob_user, …), human-written
// text becomes filler, and bot comments stay verbatim, since they are the parsing contract,
// except for the logins and names inside them. PR and KT numbers, commit SHAs, Space merge requests
// and file paths become fake values of the same shape, so a fixture can't be traced back
// to its PR.
package anonymize

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/config"
)

var pseudonyms = []string{
	"alice", "bob", "carol", "dave", "erin", "frank", "grace", "heidi", "ivan", "judy",
	"kevin", "laura", "mallory", "niaj", "olivia", "peggy", "quinn", "rupert", "sybil", "trent",
	"ursula", "victor", "walter", "xavier", "yvonne", "zoe",
}

const (
	// keptName stands for the name of a person whose login stays (the viewer, a bot).
	keptName      = "Example Name"
	commentFiller = "Comment text."
	threadFiller  = "Review comment text."
	commitFiller  = "Commit message."
	titleFiller   = "Example change"
)

var (
	// Profile links in bot comments: <a href="https://github.com/login">.
	// Underscores never occur in real logins, but synthetic test logins use them.
	profileLink = regexp.MustCompile(`https://github\.com/([A-Za-z0-9_](?:[A-Za-z0-9_-]*[A-Za-z0-9_])?)"`)
	// A person's name and login in a profile link: <a href="…"><b><code>Judy Doe (judy)</code></b></a>.
	// The name is any profile text, parentheses included, with < escaped.
	namedLogin = regexp.MustCompile(`(https://github\.com/[^"]+"[^>]*>(?:<[a-z]+>)*<code>)([^<]+?)\s\(([A-Za-z0-9_-]+(?:\[bot])?)\)</code>`)
	command    = regexp.MustCompile(`^/[a-z-]+`)
	flag       = regexp.MustCompile(`^--?[a-z-]+$`)
)

type Anonymizer struct {
	cfg  config.Config
	keep map[string]bool
	// names maps a lowercased real login to its pseudonym.
	names map[string]string
	// fakes maps real PR and KT numbers to fake ones; taken reports fake PR numbers
	// already used by fixtures outside the map.
	fakes *Map
	taken func(int) bool
	// shas and fakeSHAs are the commit SHAs seen and the stand-ins written.
	shas, fakeSHAs map[string]bool
	// issues matches the issue IDs of the configured projects.
	issues *regexp.Regexp
	// anchors are the real comment ids seen in URLs.
	anchors map[string]bool
}

// New keeps the given logins (and every viewer login it sees) verbatim.
func New(cfg config.Config, keep ...string) *Anonymizer {
	a := &Anonymizer{
		cfg: cfg, keep: map[string]bool{}, names: map[string]string{},
		fakes: NewMap(), taken: func(int) bool { return false },
		shas: map[string]bool{}, fakeSHAs: map[string]bool{},
		issues: cfg.IssuePattern(), anchors: map[string]bool{},
	}
	for _, k := range keep {
		a.keep[strings.ToLower(k)] = true
	}
	return a
}

// UseMap remaps numbers with m, which Collect extends; taken reports fake PR numbers
// that are in use without being in m.
func (a *Anonymizer) UseMap(m *Map, taken func(int) bool) {
	a.fakes, a.taken = m, taken
}

// Decode parses a response so that numbers survive unchanged.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// Encode writes a document with sorted keys and two-space indentation.
func Encode(doc any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// Collect registers the human logins of a document. Pseudonyms are assigned in the order
// logins are first seen, so call Collect for every document of a batch, in a fixed order,
// before any Rewrite: the same person then gets the same pseudonym in every fixture.
func (a *Anonymizer) Collect(doc any) {
	if viewer := lookup(doc, "data", "viewer", "login"); viewer != "" {
		a.keep[strings.ToLower(viewer)] = true
	}
	walk(doc, func(obj map[string]any) {
		a.collectIdentifiers(obj)
		if login, ok := a.humanLogin(obj); ok {
			a.register(login)
		}
		if text, ok := botText(a, obj); ok {
			for _, m := range profileLink.FindAllStringSubmatch(obj[text].(string), -1) {
				if !a.cfg.IsBot(m[1]) {
					a.register(m[1])
				}
			}
		}
	})
}

// botText names the field of obj that holds bot-written text: the body of a bot's
// comment, or the summary of a check run (the code-owners check repeats its table there).
func botText(a *Anonymizer, obj map[string]any) (string, bool) {
	if _, ok := obj["body"].(string); ok && a.botAuthored(obj) {
		return "body", true
	}
	if _, ok := obj["summary"].(string); ok && obj["__typename"] == "CheckRun" {
		return "summary", true
	}
	return "", false
}

func (a *Anonymizer) register(login string) {
	key := strings.ToLower(login)
	if a.keep[key] || a.names[key] != "" {
		return
	}
	n := len(a.names)
	name := pseudonyms[n%len(pseudonyms)]
	if round := n / len(pseudonyms); round > 0 {
		name += strconv.Itoa(round + 1)
	}
	// The underscore makes pseudonyms impossible GitHub logins.
	a.names[key] = name + "_user"
}

// Rewrite anonymizes a collected document in place. It fails if any real login
// still appears anywhere in it.
func (a *Anonymizer) Rewrite(doc any) error {
	walk(doc, func(obj map[string]any) {
		a.remap(obj)
		if login, ok := a.humanLogin(obj); ok {
			obj["login"] = a.pseudonym(login)
		}
		if _, ok := obj["headRefName"]; ok {
			a.rewritePR(obj)
		}
		if text, ok := botText(a, obj); ok {
			obj[text] = a.replaceLogins(obj[text].(string))
			return
		}
		if msg, ok := obj["message"].(string); ok {
			obj["message"] = a.commitMessage(msg)
		}
		body, ok := obj["body"].(string)
		if !ok {
			return
		}
		switch {
		case command.MatchString(strings.TrimSpace(body)):
			obj["body"] = commandOnly(body)
		case isThreadComment(obj):
			obj["body"] = threadFiller
		default:
			obj["body"] = commentFiller
		}
	})
	return a.check(doc)
}

func (a *Anonymizer) rewritePR(pr map[string]any) {
	title, _ := pr["title"].(string)
	ids := a.issues.FindAllString(title, -1)
	if len(ids) > 0 {
		pr["title"] = strings.Join(ids, ", ") + ": " + titleFiller
	} else {
		pr["title"] = titleFiller
	}
	branch, _ := pr["headRefName"].(string)
	number := fmt.Sprint(pr["number"])
	if id := a.issues.FindString(branch); id != "" {
		pr["headRefName"] = "topic/" + id + "-example"
	} else {
		pr["headRefName"] = "topic/example-" + number
	}
}

// commitMessage replaces a commit message with filler, keeping its issue trailers
// (`^KT-123 Fixed`, `^KT-123`), which the classifier reads.
func (a *Anonymizer) commitMessage(msg string) string {
	var trailers []string
	for _, line := range strings.Split(msg, "\n") {
		if a.isTrailer(line) {
			trailers = append(trailers, strings.TrimSpace(line))
		}
	}
	if len(trailers) == 0 {
		return commitFiller
	}
	return commitFiller + "\n\n" + strings.Join(trailers, "\n")
}

// isTrailer: `^<issue>` alone or followed by one word.
func (a *Anonymizer) isTrailer(line string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "^")
	if !ok {
		return false
	}
	loc := a.issues.FindStringIndex(rest)
	if loc == nil || loc[0] != 0 {
		return false
	}
	word := strings.TrimSpace(rest[loc[1]:])
	return word == "" || !strings.ContainsAny(word, " \t")
}

// commandOnly keeps the command and its flags, dropping any free text.
func commandOnly(body string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	fields := strings.Fields(first)
	out := fields[:1]
	for _, f := range fields[1:] {
		if flag.MatchString(f) {
			out = append(out, f)
		}
	}
	return strings.Join(out, " ")
}

// replaceLogins swaps logins in the places bots put them: profile links, the
// <code>login</code> inside them, and @mentions. A name next to a login in a profile link
// becomes the pseudonym's (or keptName, for a login that stays).
func (a *Anonymizer) replaceLogins(body string) string {
	body = namedLogin.ReplaceAllStringFunc(body, func(m string) string {
		g := namedLogin.FindStringSubmatch(m)
		login, name := g[3], keptName
		if pseudonym, ok := a.names[strings.ToLower(login)]; ok {
			login, name = pseudonym, pseudonymName(pseudonym)
		}
		return g[1] + name + " (" + login + ")</code>"
	})
	logins := slices.Collect(maps.Keys(a.names))
	// Longest first, so a login that prefixes another doesn't clobber it.
	slices.SortFunc(logins, func(x, y string) int { return len(y) - len(x) })
	for _, login := range logins {
		q := regexp.QuoteMeta(login)
		name := a.names[login]
		body = regexp.MustCompile(`(?i)(https://github\.com/)`+q+`"`).ReplaceAllString(body, "${1}"+name+`"`)
		body = regexp.MustCompile(`(?i)<code>`+q+`</code>`).ReplaceAllString(body, "<code>"+name+"</code>")
		body = regexp.MustCompile(`(?i)@`+q+`\b`).ReplaceAllString(body, "@"+name)
	}
	return body
}

func (a *Anonymizer) check(doc any) error {
	data, err := Encode(doc)
	if err != nil {
		return err
	}
	text := string(data)
	var leaked []string
	for login := range a.names {
		re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9-])` + regexp.QuoteMeta(login) + `([^A-Za-z0-9-]|$)`)
		if re.MatchString(text) {
			leaked = append(leaked, login)
		}
	}
	if len(leaked) > 0 {
		slices.Sort(leaked)
		return fmt.Errorf("real logins left after anonymizing: %s", strings.Join(leaked, ", "))
	}
	if ids := a.checkIdentifiers(text); len(ids) > 0 {
		slices.Sort(ids)
		return fmt.Errorf("real identifiers left after anonymizing: %s", strings.Join(ids, ", "))
	}
	return nil
}

// pseudonymName is the made-up name of a pseudonym: "Bob User" for bob_user and bob2_user.
func pseudonymName(pseudonym string) string {
	first := strings.TrimRight(strings.TrimSuffix(pseudonym, "_user"), "0123456789")
	return strings.ToUpper(first[:1]) + first[1:] + " User"
}

func (a *Anonymizer) pseudonym(login string) string {
	if name, ok := a.names[strings.ToLower(login)]; ok {
		return name
	}
	return login
}

// humanLogin returns the login of an actor object that isn't a bot or kept.
func (a *Anonymizer) humanLogin(obj map[string]any) (string, bool) {
	login, ok := obj["login"].(string)
	if !ok || login == "" || a.keep[strings.ToLower(login)] || a.isBot(obj) {
		return "", false
	}
	return login, true
}

func (a *Anonymizer) isBot(actor map[string]any) bool {
	login, _ := actor["login"].(string)
	return actor["__typename"] == "Bot" || a.cfg.IsBot(login)
}

func (a *Anonymizer) botAuthored(obj map[string]any) bool {
	author, ok := obj["author"].(map[string]any)
	return ok && a.isBot(author)
}

// isThreadComment: review-thread comments carry no updatedAt/isMinimized, PR comments do.
func isThreadComment(obj map[string]any) bool {
	_, ok := obj["isMinimized"]
	return !ok
}

// walk calls f for every object, visiting keys in sorted order so that Collect is deterministic.
func walk(v any, f func(map[string]any)) {
	switch v := v.(type) {
	case map[string]any:
		f(v)
		for _, k := range slices.Sorted(maps.Keys(v)) {
			walk(v[k], f)
		}
	case []any:
		for _, e := range v {
			walk(e, f)
		}
	}
}

// FileName is the fixture file name of an anonymized PR response: pr-<fake number>.json.
func FileName(doc any) string {
	n := doc
	for _, p := range []string{"data", "repository", "pullRequest", "number"} {
		m, _ := n.(map[string]any)
		n = m[p]
	}
	return fmt.Sprintf("pr-%v.json", n)
}

func lookup(v any, path ...string) string {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v = m[p]
	}
	s, _ := v.(string)
	return s
}
