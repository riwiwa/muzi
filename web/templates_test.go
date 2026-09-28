package web

import (
	"os"
	"testing"
)

// Every template must parse with the real function map; a typo in a .gohtml file fails here
// instead of when the page is first requested
func TestTemplatesParse(t *testing.T) {
	if err := Init(os.DirFS("../templates"), os.DirFS("../static")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"base", "profile", "artist", "album", "song", "settings", "scrobble", "grid",
		"login.gohtml", "create_account.gohtml", "import.gohtml"} {
		if templates.Lookup(name) == nil {
			t.Errorf("template %q is missing", name)
		}
	}
}
