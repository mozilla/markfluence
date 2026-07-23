package read

import "testing"

func TestParsePageID(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string // "" means expect an error
	}{
		{"bare numeric id", "123456", "123456"},
		{"modern path with slug", "https://org.atlassian.net/wiki/spaces/ENG/pages/123456/Some+Title", "123456"},
		{"modern path no slug", "https://org.atlassian.net/wiki/spaces/ENG/pages/123456", "123456"},
		{"legacy pageId query", "https://org.atlassian.net/wiki/pages/viewpage.action?pageId=987654", "987654"},
		{"query wins over path only if numeric", "https://org.atlassian.net/wiki/x?pageId=42", "42"},
		{"non-numeric arg", "not-a-page", ""},
		{"url without an id", "https://org.atlassian.net/wiki/spaces/ENG/overview", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePageID(tt.arg)
			if tt.want == "" {
				if err == nil {
					t.Fatalf("parsePageID(%q) = %q, nil; want error", tt.arg, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parsePageID(%q) = %q, %v; want %q", tt.arg, got, err, tt.want)
			}
		})
	}
}
