package tasks

import (
	"sort"
	"strings"
	"unicode"
)

const RedactedPlaceholder = "***"

type RedactionPolicy struct {
	Names  map[string]struct{}
	Tokens map[string]struct{}
}

func DefaultRedactionPolicy() RedactionPolicy {
	return NewRedactionPolicy(
		[]string{"PGPASSWORD", "MYSQL_PWD", "DATABASE_URL", "KUBECONFIG"},
		[]string{"SECRET", "TOKEN", "KEY", "PASSWORD", "PASSWD", "CREDENTIAL", "AUTH", "SESSION", "COOKIE", "BEARER", "JWT", "SIGNATURE", "PRIVATE"},
	)
}

func NewRedactionPolicy(names []string, tokens []string) RedactionPolicy {
	policy := RedactionPolicy{
		Names:  make(map[string]struct{}),
		Tokens: make(map[string]struct{}),
	}
	for _, name := range names {
		if normalized := normalizeRedactionName(name); normalized != "" {
			policy.Names[normalized] = struct{}{}
		}
	}
	for _, token := range tokens {
		if normalized := normalizeRedactionName(token); normalized != "" {
			policy.Tokens[normalized] = struct{}{}
		}
	}
	return policy
}

func MergeRedactionPolicies(items ...RedactionPolicy) RedactionPolicy {
	merged := RedactionPolicy{
		Names:  make(map[string]struct{}),
		Tokens: make(map[string]struct{}),
	}
	for _, item := range items {
		for name := range item.Names {
			merged.Names[name] = struct{}{}
		}
		for token := range item.Tokens {
			merged.Tokens[token] = struct{}{}
		}
	}
	return merged
}

func (p RedactionPolicy) ShouldRedact(name string) bool {
	normalized := normalizeRedactionName(name)
	if normalized == "" {
		return false
	}
	if _, ok := p.Names[normalized]; ok {
		return true
	}
	for _, token := range tokenizeRedactionName(normalized) {
		if _, ok := p.Tokens[token]; ok {
			return true
		}
	}
	return false
}

func (p RedactionPolicy) SortedNames() []string {
	items := make([]string, 0, len(p.Names))
	for name := range p.Names {
		items = append(items, name)
	}
	sort.Strings(items)
	return items
}

func (p RedactionPolicy) SortedTokens() []string {
	items := make([]string, 0, len(p.Tokens))
	for token := range p.Tokens {
		items = append(items, token)
	}
	sort.Strings(items)
	return items
}

func normalizeRedactionName(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func tokenizeRedactionName(name string) []string {
	var items []string
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		items = append(items, builder.String())
		builder.Reset()
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return items
}
