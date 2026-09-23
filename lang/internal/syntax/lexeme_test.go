package syntax

import (
	"slices"
	"strings"
	"testing"
	"unicode"
)

var lexemeCorpus = []string{
	`if(a, b, add(1, 1))`,
	`amount * bps / 10_000 + fixed // a comment`,
	`switch(country, case "SG", "MY" => 1, else => 2)`,
	`switch(case amount > 10_000 => "manual_review", else "auto")`,
	`[{channel: c, currency: k} for c in channels if healthy(c) for k in currencies]`,
	`{k: v * 2 for k, v in rates if v > 0}`,
	`reduce(name, weight in weights, total = 0.0, total + weight)`,
	`let(bps = 250, rate = -bps, order.items[0].price * rate)`,
	`route.score_v1(order).fee != -1.5 && !(x in xs)`,
	`switch(channel, case @adyen => @channel.stripe, case @stripe => @adyen)`,
	"{\"k\": 1,\n // between\n \"j\": 2}",
	`f(a, `, `a + @`, `"unterminated`, `a ¥ b`, `1.`, `let(x = 1)`, ``, `// only a comment`,
	// A name read inside parentheses, and a dotted name with an empty part,
	// once sent the lexemes round in a loop that never ended.
	`(b)`, `f((x))`, `-(b)`, `a..b`, `f(a..b)`, `a.b..c`, "\"d.\x18\"",
	// A lone byte that is not UTF-8 is not whitespace, whatever it would be as a rune.
	"a \x85 b",
}

// Every piece of source is accounted for exactly once: the lexemes are in
// order, do not overlap, and what lies between them is whitespace.
func TestLexemesCoverTheSource(t *testing.T) {
	t.Parallel()
	for _, source := range lexemeCorpus {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			checkLexemesCover(t, source)
		})
	}
}

// checkLexemesCover fails unless source's lexemes are in order, do not
// overlap, and leave nothing but whitespace between them.
func checkLexemesCover(t testing.TB, source string) {
	t.Helper()
	lexemes, _ := Lexemes(source)
	at := 0
	for _, lexeme := range lexemes {
		if lexeme.Start < at || lexeme.End <= lexeme.Start {
			t.Fatalf("%q: lexeme %+v overlaps or is empty", source, lexeme)
		}
		if gap := source[at:lexeme.Start]; strings.TrimFunc(gap, unicode.IsSpace) != "" {
			t.Fatalf("%q: %q is in no lexeme", source, gap)
		}
		at = lexeme.End
	}
	if rest := source[at:]; strings.TrimFunc(rest, unicode.IsSpace) != "" {
		t.Fatalf("%q: %q is in no lexeme", source, rest)
	}
}

// The error is the one Parse reports, and the pieces the parser got through
// before it keep their roles.
func TestLexemesReportWhatParseReports(t *testing.T) {
	t.Parallel()
	for _, source := range lexemeCorpus {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, lexErr := Lexemes(source)
			_, parseErr := Parse(source)
			if (lexErr == nil) != (parseErr == nil) || lexErr != nil && lexErr.Error() != parseErr.Error() {
				t.Errorf("%q: Lexemes says %v, Parse says %v", source, lexErr, parseErr)
			}
		})
	}
	roles := rolesOf(t, `f(a, `)
	if roles["f"] != RoleFunction || roles["a"] != RoleVariable {
		t.Errorf("an unfinished call lost what was known: roles of %q = %v, want f %q and a %q", `f(a, `, roles, RoleFunction, RoleVariable)
	}
	lexemes, _ := Lexemes(`a ¥ b`)
	if invalid := lexemes[1]; invalid.Class != ClassInvalid || invalid.End-invalid.Start != len("¥") {
		t.Errorf("Lexemes(%q)[1] = %+v, want an invalid lexeme over %q", `a ¥ b`, invalid, "¥")
	}
}

func TestLexemesSayWhatEachPieceIs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		source string
		want   map[string]Role
	}{
		{`if(a, b, c)`, map[string]Role{"if": RoleFunction, "a": RoleArgument}},
		{`let(r = f, [r * x for x in xs])`, map[string]Role{"f": RoleArgument, "xs": RoleArgument}},
		{`[x for x in xs if x > 0]`, map[string]Role{"for": RoleKeyword, "in": RoleKeyword, "if": RoleKeyword, ">": RoleOperator, "0": RoleLiteral}},
		{`x in xs`, map[string]Role{"in": RoleOperator}},
		{`let(rate = 2, rate)`, map[string]Role{"let": RoleForm, "=": RoleNone, "2": RoleLiteral}},
		{`reduce(p in ps, acc = 0, acc + p)`, map[string]Role{"reduce": RoleForm, "in": RoleKeyword, "+": RoleOperator}},
		{`switch(x, case 1 => true, else false)`, map[string]Role{"switch": RoleForm, "case": RoleKeyword, "else": RoleKeyword, "true": RoleLiteral}},
		{`route.score_v1(order)`, map[string]Role{"route.score_v1": RoleFunction, "order": RoleArgument}},
		{`order.items`, map[string]Role{"order": RoleArgument, "items": RoleField}},
		{`f(x).fee`, map[string]Role{"fee": RoleField}},
		{`{amount: 1, currency: "SGD"}`, map[string]Role{"amount": RoleField, "currency": RoleField, `"SGD"`: RoleLiteral}},
		{`{"k": v}`, map[string]Role{`"k"`: RoleLiteral, "v": RoleArgument}},
		{`@channel.adyen == c`, map[string]Role{"@channel.adyen": RoleEnumMember, "==": RoleOperator}},
		{`-x`, map[string]Role{"-": RoleOperator, "x": RoleArgument}},
		{`-1`, map[string]Role{"-": RoleOperator, "1": RoleLiteral}},
	}
	for _, c := range cases {
		t.Run(c.source, func(t *testing.T) {
			t.Parallel()
			checkRoles(t, c.source, c.want)
		})
	}
	reads := map[string][]Role{}
	source := `let(r = f, [r * x for x in xs])`
	lexemes, _ := Lexemes(source)
	for _, lexeme := range lexemes {
		text := source[lexeme.Start:lexeme.End]
		reads[text] = append(reads[text], lexeme.Role)
	}
	if want := []Role{RoleLocal, RoleLocalRead}; !slices.Equal(reads["r"], want) || !slices.Equal(reads["x"], []Role{RoleLocalRead, RoleLocal}) {
		t.Errorf("locals in %q are read as r %v and x %v, want %v and %v", source, reads["r"], reads["x"], want, []Role{RoleLocalRead, RoleLocal})
	}
	lexemes, _ = Lexemes(`order.items // note`)
	if lexemes[1].Class != ClassPunctuation || lexemes[3].Class != ClassComment {
		t.Errorf("Lexemes(%q) = %+v, want the dot as %q and the comment as %q", `order.items // note`, lexemes, ClassPunctuation, ClassComment)
	}
}

// checkRoles fails unless each text in want is read with its role.
func checkRoles(t *testing.T, source string, want map[string]Role) {
	t.Helper()
	roles := rolesOf(t, source)
	for text, role := range want {
		if got, ok := roles[text]; !ok || got != role {
			t.Errorf("%s: %q is %q, want %q (%v)", source, text, got, role, roles)
		}
	}
}

// rolesOf maps each piece's text to its role. A text that occurs twice must
// have one role both times, or the test would be reading the wrong piece.
func rolesOf(t *testing.T, source string) map[string]Role {
	t.Helper()
	lexemes, _ := Lexemes(source)
	out := map[string]Role{}
	for _, lexeme := range lexemes {
		text := source[lexeme.Start:lexeme.End]
		if prior, seen := out[text]; seen && prior != lexeme.Role {
			out[text] = "ambiguous"
			continue
		}
		out[text] = lexeme.Role
	}
	return out
}
