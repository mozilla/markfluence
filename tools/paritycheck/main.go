// Command paritycheck compares the Python and Go regression outputs for the
// markdown converter and reports, per case, whether they are semantically the
// same. It is a manual-review aid used during the Go port (phase 1), not a CI
// gate: attachments/broken/warnings are compared exactly, while the html field
// is compared semantically (parsed as XML, attributes sorted, insignificant
// whitespace normalized), so cosmetic differences (whitespace, attribute order)
// are tolerated and only structural differences are flagged.
package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

const (
	pythonDir = "tests/regression"
	goDir     = "internal/convert/testdata/regression"
)

type page struct {
	Attachments []map[string]string `json:"attachments"`
	Broken      []string            `json:"broken"`
	HTML        string              `json:"html"`
	Warnings    []string            `json:"warnings"`
}

type verdict int

const (
	identical verdict = iota
	cosmetic
	structural
	missing
)

func main() {
	verbose := flag.Bool("v", false, "print diffs for structural mismatches")
	flag.Parse()

	cases, err := caseNames()
	if err != nil {
		fmt.Fprintln(os.Stderr, "paritycheck:", err)
		os.Exit(2)
	}

	fmt.Printf("paritycheck: Python (%s) vs Go (%s)\n\n", pythonDir, goDir)
	var counts [4]int
	for _, name := range cases {
		v, detail := compareCase(name)
		counts[v]++
		fmt.Printf("  %-32s %s\n", name, label(v))
		if *verbose && detail != "" {
			fmt.Println(indent(detail, "      "))
		}
	}

	fmt.Printf("\n%d cases: %s, %s, %s, %s\n",
		len(cases),
		green.Render(fmt.Sprintf("%d identical", counts[identical])),
		green.Render(fmt.Sprintf("%d cosmetic", counts[cosmetic])),
		red.Render(fmt.Sprintf("%d structural", counts[structural])),
		yellow.Render(fmt.Sprintf("%d missing", counts[missing])),
	)
	if !*verbose && counts[structural] > 0 {
		fmt.Println("(re-run with -v to see structural diffs)")
	}
}

// caseNames returns the union of case directory names across both trees, sorted.
func caseNames() ([]string, error) {
	seen := map[string]bool{}
	for _, dir := range []string{pythonDir, goDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasPrefix(e.Name(), "_") {
				seen[e.Name()] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func compareCase(name string) (verdict, string) {
	pyBytes, errP := os.ReadFile(filepath.Join(pythonDir, name, "test.output"))
	goBytes, errG := os.ReadFile(filepath.Join(goDir, name, "test.output"))
	if errP != nil || errG != nil {
		return missing, fmt.Sprintf("python: %v\ngo: %v", errP, errG)
	}
	var py, gp page
	if err := json.Unmarshal(pyBytes, &py); err != nil {
		return structural, "python output not valid JSON: " + err.Error()
	}
	if err := json.Unmarshal(goBytes, &gp); err != nil {
		return structural, "go output not valid JSON: " + err.Error()
	}

	var diffs []string
	if !sliceEqual(py.Attachments, gp.Attachments) {
		diffs = append(diffs, fmt.Sprintf("attachments differ:\n  python: %v\n  go:     %v", py.Attachments, gp.Attachments))
	}
	if !stringsEqual(py.Broken, gp.Broken) {
		diffs = append(diffs, fmt.Sprintf("broken differ:\n  python: %v\n  go:     %v", py.Broken, gp.Broken))
	}
	if !stringsEqual(py.Warnings, gp.Warnings) {
		diffs = append(diffs, fmt.Sprintf("warnings differ:\n  python: %v\n  go:     %v", py.Warnings, gp.Warnings))
	}

	htmlDiff := compareHTML(py.HTML, gp.HTML)
	if htmlDiff != "" {
		diffs = append(diffs, htmlDiff)
	}

	if len(diffs) > 0 {
		return structural, strings.Join(diffs, "\n")
	}
	if py.HTML == gp.HTML {
		return identical, ""
	}
	return cosmetic, ""
}

// compareHTML returns "" when the two fragments are semantically equal, otherwise
// a human-readable diff. It parses each as an XML fragment and compares the
// canonical form; if either won't parse, it falls back to whitespace-normalized
// string comparison.
func compareHTML(pyHTML, goHTML string) string {
	pyc, errP := canonicalHTML(pyHTML)
	goc, errG := canonicalHTML(goHTML)
	if errP != nil || errG != nil {
		if normalizeWS(pyHTML) == normalizeWS(goHTML) {
			return ""
		}
		return fmt.Sprintf("html differs (string-normalized fallback; xml parse: py=%v go=%v):\n  python: %s\n  go:     %s",
			errP, errG, normalizeWS(pyHTML), normalizeWS(goHTML))
	}
	if pyc == goc {
		return ""
	}
	return fmt.Sprintf("html differs structurally:\n  python: %s\n  go:     %s", pyc, goc)
}

// canonicalHTML normalizes a storage-format fragment for semantic comparison:
// attributes sorted, runs of insignificant whitespace collapsed. The fragment is
// wrapped in a synthetic root so it parses as a single XML document.
func canonicalHTML(fragment string) (string, error) {
	dec := xml.NewDecoder(strings.NewReader("<root>" + fragment + "</root>"))
	dec.Strict = false
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			b.WriteString("<" + xmlName(t.Name))
			attrs := make([]xml.Attr, len(t.Attr))
			copy(attrs, t.Attr)
			sort.Slice(attrs, func(i, j int) bool { return xmlName(attrs[i].Name) < xmlName(attrs[j].Name) })
			for _, a := range attrs {
				b.WriteString(" " + xmlName(a.Name) + `="` + a.Value + `"`)
			}
			b.WriteString(">")
		case xml.EndElement:
			b.WriteString("</" + xmlName(t.Name) + ">")
		case xml.CharData:
			if s := strings.Join(strings.Fields(string(t)), " "); s != "" {
				b.WriteString(s)
			}
		}
	}
	return b.String(), nil
}

func xmlName(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

func normalizeWS(s string) string { return strings.Join(strings.Fields(s), " ") }

// label renders a colored, symboled indicator for a verdict. Green marks
// semantic equivalence (identical or cosmetic -- the goal), red marks a
// structural difference (work remaining), and yellow marks a missing output.
// The leading symbol keeps the meaning clear without color (piped / NO_COLOR).
func label(v verdict) string {
	switch v {
	case identical:
		return green.Render("✓ identical")
	case cosmetic:
		return green.Render("✓ cosmetic")
	case missing:
		return yellow.Render("! missing")
	default:
		return red.Render("✗ structural")
	}
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+prefix)
}

func sliceEqual(a, b []map[string]string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func stringsEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
