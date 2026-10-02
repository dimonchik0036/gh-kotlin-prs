package config

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Action is something a key does in the TUI.
type Action struct {
	Name string
	Doc  string
	// Keys are the default bindings, named as the TUI reports them: "a", "G", "?",
	// "enter", "esc", "tab", "shift+tab", "up", "pgdown", "ctrl+x", "space".
	Keys []string
}

// Actions lists every TUI action with its default keys, in the order of the help.
var Actions = []Action{
	{"up", "select the row above (scroll up in the details)", []string{"up", "k"}},
	{"down", "select the row below (scroll down in the details)", []string{"down", "j"}},
	{"first", "select the first row", []string{"home", "g"}},
	{"last", "select the last row", []string{"end", "G"}},
	{"pageUp", "a page up", []string{"pgup"}},
	{"pageDown", "a page down", []string{"pgdown"}},
	{"nextSection", "the first row of the next section", []string{"tab"}},
	{"previousSection", "the first row of the previous section", []string{"shift+tab"}},
	{"details", "the details of the selected PR", []string{"enter"}},
	{"back", "back to the list, or clear the filter", []string{"esc", "backspace"}},
	{"filter", "filter the rows by their text", []string{"/"}},
	{"all", "also show reviews not waiting on you and drafts", []string{"a"}},
	{"open", "open the PR in the browser", []string{"o"}},
	{"build", "open its newest build (or the bot comment)", []string{"b"}},
	{"copy", "copy its URL", []string{"y"}},
	{"copyBranch", "copy its branch name", []string{"Y"}},
	{"actions", "the commands that can be posted on it now", []string{"x"}},
	{"dryRun", "post /dry-run on it, after asking", []string{"D"}},
	{"dryRunRetry", "post /dry-run --retry, after asking", []string{"R"}},
	{"safeMerge", "post /safe-merge, after asking", []string{"M"}},
	{"cancelCoordinator", "post /cancel-coordinator, after asking", []string{"C"}},
	{"fixup", "post /fixup, after asking", []string{"F"}},
	{"codeowners", "post /codeowners, after asking", []string{"O"}},
	{"requestReview", "pick code owners to request a review from, then send", []string{"A"}},
	{"refresh", "refresh now", []string{"r"}},
	{"help", "this help", []string{"?"}},
	{"quit", "quit (ctrl+c always quits)", []string{"q"}},
}

// Keys maps every action to its keys: the defaults, with the config's overrides.
type Keys map[string][]string

// DefaultKeys are the bindings of Actions.
func DefaultKeys() Keys {
	keys := Keys{}
	for _, a := range Actions {
		keys[a.Name] = slices.Clone(a.Keys)
	}
	return keys
}

// UnmarshalYAML replaces the keys of the actions the node names: one key, or a list.
func (k *Keys) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: keys must map actions to keys", node.Line)
	}
	merged := DefaultKeys()
	for name, keys := range *k {
		merged[name] = keys
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name, value := node.Content[i].Value, node.Content[i+1]
		if !slices.ContainsFunc(Actions, func(a Action) bool { return a.Name == name }) {
			return fmt.Errorf("line %d: keys: unknown action %q", node.Content[i].Line, name)
		}
		var keys []string
		switch value.Kind {
		case yaml.ScalarNode:
			keys = []string{value.Value}
		case yaml.SequenceNode:
			if err := value.Decode(&keys); err != nil {
				return fmt.Errorf("line %d: keys: %s: %w", value.Line, name, err)
			}
		default:
			return fmt.Errorf("line %d: keys: %s must be a key or a list of keys", value.Line, name)
		}
		merged[name] = keys
	}
	*k = merged
	return nil
}

// validate rejects actions without keys, blank keys, and a key bound twice.
func (k *Keys) validate() error {
	owner := map[string]string{}
	for _, a := range Actions {
		keys := (*k)[a.Name]
		if len(keys) == 0 {
			return fmt.Errorf("config: keys: %s has no key", a.Name)
		}
		for _, key := range keys {
			switch other, taken := owner[key]; {
			case strings.TrimSpace(key) == "" || strings.ContainsAny(key, " \t"):
				return fmt.Errorf("config: keys: %s: %q is not a key name", a.Name, key)
			case key == "ctrl+c":
				return fmt.Errorf("config: keys: %s: ctrl+c always quits", a.Name)
			case taken && other == a.Name:
				return fmt.Errorf("config: keys: %s lists %q twice", a.Name, key)
			case taken:
				return fmt.Errorf("config: keys: %q is bound to both %s and %s", key, other, a.Name)
			}
			owner[key] = a.Name
		}
	}
	return nil
}
