package completion

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

func TestMarkdownFiles(t *testing.T) {
	values, directive := MarkdownFiles(nil, nil, "")
	if !slices.Equal(values, []string{"md"}) {
		t.Errorf("values = %v, want [md]", values)
	}
	if directive != cobra.ShellCompDirectiveFilterFileExt {
		t.Errorf("directive = %v, want ShellCompDirectiveFilterFileExt", directive)
	}
}

func TestPageThenFiles(t *testing.T) {
	values, directive := PageThenFiles(nil, nil, "")
	if !slices.Equal(values, []string{"md"}) || directive != cobra.ShellCompDirectiveFilterFileExt {
		t.Errorf("PAGE position = (%v, %v), want ([md], FilterFileExt)", values, directive)
	}

	// The uploads after PAGE are attachments of any type, so completion hands
	// the position back to the shell's own file completion.
	values, directive = PageThenFiles(nil, []string{"123"}, "")
	if values != nil || directive != cobra.ShellCompDirectiveDefault {
		t.Errorf("FILE position = (%v, %v), want (nil, Default)", values, directive)
	}
}

func TestPageThenNames(t *testing.T) {
	values, directive := PageThenNames(nil, nil, "")
	if !slices.Equal(values, []string{"md"}) || directive != cobra.ShellCompDirectiveFilterFileExt {
		t.Errorf("PAGE position = (%v, %v), want ([md], FilterFileExt)", values, directive)
	}

	// An attachment name lives on the server; local filenames would be wrong.
	values, directive = PageThenNames(nil, []string{"123"}, "")
	if values != nil || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("NAME position = (%v, %v), want (nil, NoFileComp)", values, directive)
	}
}

func TestDirectories(t *testing.T) {
	values, directive := Directories(nil, nil, "")
	if values != nil || directive != cobra.ShellCompDirectiveFilterDirs {
		t.Errorf("Directories() = (%v, %v), want (nil, FilterDirs)", values, directive)
	}
}

func TestValues(t *testing.T) {
	values, directive := Values("narrow", "wide", "max")(nil, nil, "")
	if !slices.Equal(values, []string{"narrow", "wide", "max"}) {
		t.Errorf("values = %v, want [narrow wide max]", values)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", directive)
	}
}

func TestRegisterFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	cmd.Flags().String("width", "", "")
	RegisterFlag(cmd, "width", Values("narrow"))

	complete, ok := cmd.GetFlagCompletionFunc("width")
	if !ok {
		t.Fatal("no completion function registered for --width")
	}
	if got, _ := complete(cmd, nil, ""); !slices.Equal(got, []string{"narrow"}) {
		t.Errorf("completion values = %v, want [narrow]", got)
	}
}

func TestRegisterFlagPanicsOnUnknownFlag(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a completion for a nonexistent flag did not panic")
		}
	}()
	RegisterFlag(&cobra.Command{Use: "demo"}, "nope", Values("x"))
}
