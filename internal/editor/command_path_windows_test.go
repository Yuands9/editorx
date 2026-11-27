package editor

import "testing"

func TestEditorCommandPath(t *testing.T) {
	const directory = `D:\software\EditorX`
	const old = `C:\old\EditorX`
	for _, test := range []struct {
		path, previous, want string
		expandable           bool
	}{
		{"", "", editorCommandHomeReference, true},
		{`C:\bin;%USERPROFILE%\bin`, "", `C:\bin;%USERPROFILE%\bin;%EDITORX_HOME%`, true},
		{`C:\bin;%EDITORX_HOME%`, old, `C:\bin;%EDITORX_HOME%`, true},
		{`C:\bin;"D:\software\editorx"`, "", `C:\bin;"D:\software\editorx"`, true},
		{`%UNEXPANDED%;` + old, old, `%UNEXPANDED%;` + directory, false},
	} {
		if got := editorCommandPath(test.path, test.previous, directory, test.expandable); got != test.want {
			t.Fatalf("PATH %q -> %q, want %q", test.path, got, test.want)
		}
	}
}
