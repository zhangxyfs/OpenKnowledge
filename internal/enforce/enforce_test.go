package enforce

import (
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/state"
)

var rule = config.EnforceRule{
	Type:          "changelog_required",
	CodeGlobs:     []string{"**/*.go"},
	ChangelogGlob: "docs/changelogs/**",
	Message:       "请补变更日志",
}

func TestBlockWhenCodeTouchedWithoutChangelog(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	block, reason := EvalChangelog(rule, st)
	if !block || reason != "请补变更日志" {
		t.Fatalf("expected block, got %v %q", block, reason)
	}
}

func TestNoBlockWhenChangelogTouched(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	st.AddTouched("docs/changelogs/2026-07-22.md")
	block, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("expected no block")
	}
}

func TestNoBlockWhenNoCodeTouched(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("README.md")
	block, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("expected no block")
	}
}

func TestBlockOnlyOncePerSession(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	st.MarkBlocked("changelog_required")
	block, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("rule already blocked once this session")
	}
}

func TestRootLevelGoFileMatches(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("main.go")
	block, _ := EvalChangelog(rule, st)
	if !block {
		t.Fatal("**/*.go should match root-level main.go")
	}
}
