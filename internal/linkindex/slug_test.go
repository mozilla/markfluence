package linkindex

import "testing"

func TestGithubSlug(t *testing.T) {
	cases := map[string]string{
		"Hello World":        "hello-world",
		"Hello, World!":      "hello-world",
		"  Leading/trailing": "leadingtrailing",
		" Hello ":            "hello",
		"Café Menu":          "café-menu", // Unicode letters are preserved, not stripped
		"under_score":        "under_score",
		"":                   "",
		"---":                "", // trims to nothing
	}
	for in, want := range cases {
		if got := GithubSlug(in); got != want {
			t.Errorf("GithubSlug(%q) = %q, want %q", in, got, want)
		}
	}
	// Each whitespace character becomes its own hyphen -- runs are not
	// collapsed, unlike ConfluenceSlug.
	if got := GithubSlug("Hello   World"); got != "hello---world" {
		t.Errorf(`GithubSlug("Hello   World") = %q, want "hello---world"`, got)
	}
}

func TestConfluenceSlug(t *testing.T) {
	// Case and punctuation survive; only whitespace becomes a hyphen.
	if got := ConfluenceSlug("Hello, World!"); got != "Hello,-World!" {
		t.Errorf(`ConfluenceSlug("Hello, World!") = %q, want "Hello,-World!"`, got)
	}
	// Runs of whitespace collapse to a single hyphen -- the opposite of
	// GithubSlug, which hyphenates each whitespace character individually.
	if got := ConfluenceSlug("Hello   World"); got != "Hello-World" {
		t.Errorf(`ConfluenceSlug("Hello   World") = %q, want "Hello-World"`, got)
	}
	// Leading/trailing whitespace is trimmed before collapsing, not turned
	// into a leading/trailing hyphen.
	if got := ConfluenceSlug("  Hello World  "); got != "Hello-World" {
		t.Errorf(`ConfluenceSlug("  Hello World  ") = %q, want "Hello-World"`, got)
	}
}

func TestExtractHeadings(t *testing.T) {
	body := "# Title\n" +
		"some text\n" +
		"## Sub Heading\n" +
		"```\n" +
		"# not a heading, inside a fence\n" +
		"```\n" +
		"### Real Heading\n" +
		"#NoSpaceAfterHash\n" +
		"###\n" + // all hashes, nothing else
		"# \n" // hash then only whitespace -- trims to empty

	got := extractHeadings(body)
	want := []string{"Title", "Sub Heading", "Real Heading"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("heading %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractHeadingsUnterminatedFenceSkipsToEnd(t *testing.T) {
	// A fence that's never closed must not leave later real headings exposed
	// by some off-by-one toggle; everything after the open fence is "in code."
	body := "# Before\n```\n# inside, unterminated\n## also inside\n"
	got := extractHeadings(body)
	if len(got) != 1 || got[0] != "Before" {
		t.Errorf("got %v, want just [Before]", got)
	}
}
