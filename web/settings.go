package web

import "net/http"

func settingsPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		type data struct {
			Title            string
			LoggedInUsername string
			TemplateName     string
		}
		d := data{
			Title:            "muzi | Settings",
			LoggedInUsername: username,
			TemplateName:     "settings",
		}
		err := templates.ExecuteTemplate(w, "base", d)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
