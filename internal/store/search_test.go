package store

import (
	"strings"
	"testing"
)

func TestLikeEscapeMakesWildcardsLiteral(t *testing.T) {
	if got := likeEscape(`50%_a\b`); got != `50\%\_a\\b` {
		t.Fatalf("likeEscape = %q", got)
	}
}

func TestSearchQueryScopesSitesToTheViewersOrg(t *testing.T) {
	where, args := filterSQL(Viewer{OrgID: "org_a"}, Filter{Query: "Navarcles"})
	sql := strings.Join(where, " AND ")
	for _, want := range []string{"body->>'road' ILIKE", "body->>'mix' ILIKE", "FROM sites WHERE (name ILIKE", "org_id = $"} {
		if !strings.Contains(sql, want) {
			t.Errorf("3+ character search should contain %q: %s", want, sql)
		}
	}
	if len(args) < 6 {
		t.Fatalf("expected the term for each column plus the org, got %d args", len(args))
	}
	// Two characters only match ids and references, from the start: cheap while typing.
	where, _ = filterSQL(Viewer{OrgID: "org_a"}, Filter{Query: "FU"})
	if strings.Contains(strings.Join(where, " "), "sites") {
		t.Fatal("a two-character search should not look at sites")
	}
}
