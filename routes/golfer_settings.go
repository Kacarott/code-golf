package routes

import (
	"net/http"

	"github.com/code-golf/code-golf/config"
	"github.com/code-golf/code-golf/null"
	"github.com/code-golf/code-golf/oauth"
	"github.com/code-golf/code-golf/session"
	"github.com/code-golf/code-golf/zone"
)

// POST /golfer/cancel-delete
func golferCancelDeletePOST(w http.ResponseWriter, r *http.Request) {
	session.Database(r).MustExec(
		"UPDATE users SET delete = NULL WHERE id = $1",
		session.Golfer(r).ID,
	)

	http.Redirect(w, r, "/golfer/settings", http.StatusSeeOther)
}

// POST /golfer/delete
func golferDeletePOST(w http.ResponseWriter, r *http.Request) {
	session.Database(r).MustExec(
		"UPDATE users SET delete = TIMEZONE('UTC', NOW()) + INTERVAL '7 days' WHERE id = $1",
		session.Golfer(r).ID,
	)

	http.Redirect(w, r, "/golfer/settings", http.StatusSeeOther)
}

// GET /golfer/settings
func golferSettingsGET(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Connections    []oauth.Connection
		Countries      map[string][]*config.Country
		Layouts        []string
		Keymaps        []string
		OAuthProviders map[string]*oauth.Config
		OAuthState     string
		Pronouns       []string
		Themes         []string
		TimeZones      []zone.Zone
	}{
		oauth.GetConnections(session.Database(r), session.Golfer(r).ID, false),
		config.CountryTree,
		[]string{"default", "tabs"},
		[]string{"default", "vim"},
		oauth.Providers,
		nonce(),
		[]string{"he/him", "she/her", "they/them"},
		[]string{"auto", "dark", "light"},
		zone.List(),
	}

	http.SetCookie(w, &http.Cookie{
		HttpOnly: true,
		Name:     "__Host-oauth-state",
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
		Value:    data.OAuthState,
	})

	render(w, r, "golfer/settings", data, "Settings")
}

// POST /golfer/settings/{page}
func golferSettingsPagePOST(w http.ResponseWriter, r *http.Request) {
	golfer := session.Golfer(r)
	page := param(r, "page")

	// If the posted value is valid, update the golfer's settings map.
	for _, setting := range config.Settings[page] {
		if value := r.FormValue(setting.ID); setting.ValidValue(value) {
			golfer.Settings[page][setting.ID] = value
		}
	}

	golfer.SaveSettings(session.Database(r))

	// TODO Redirect based on page. Note page hasn't been validated yet.
	http.Redirect(w, r, "/", http.StatusFound)
}

// POST /golfer/settings/{page}/reset
func golferSettingsPageResetPOST(w http.ResponseWriter, r *http.Request) {
	golfer := session.Golfer(r)

	delete(golfer.Settings, param(r, "page"))

	golfer.SaveSettings(session.Database(r))

	// TODO Redirect based on page. Note page hasn't been validated yet.
	http.Redirect(w, r, "/", http.StatusFound)
}

// POST /golfer/settings
func golferSettingsPOST(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	if c := r.Form.Get("country"); c != "" && config.CountryByID[c] == nil {
		http.Error(w, "Invalid country", http.StatusBadRequest)
		return
	}

	if k := r.Form.Get("layout"); k != "default" && k != "tabs" {
		http.Error(w, "Invalid layout", http.StatusBadRequest)
		return
	}

	if k := r.Form.Get("keymap"); k != "default" && k != "vim" {
		http.Error(w, "Invalid keymap", http.StatusBadRequest)
		return
	}

	switch r.Form.Get("pronouns") {
	case "", "he/him", "she/her", "they/them":
	default:
		http.Error(w, "Invalid pronouns", http.StatusBadRequest)
		return
	}

	if t := r.Form.Get("theme"); t != "auto" && t != "dark" && t != "light" {
		http.Error(w, "Invalid theme", http.StatusBadRequest)
		return
	}

	if _, ok := zone.ByID[r.Form.Get("time_zone")]; !ok && r.Form.Get("time_zone") != "" {
		http.Error(w, "Invalid time_zone", http.StatusBadRequest)
		return
	}

	tx := session.Database(r).MustBeginTx(r.Context(), nil)
	defer tx.Rollback()

	userID := session.Golfer(r).ID
	tx.MustExec(
		`UPDATE users
		    SET country = $1,
		         layout = $2,
		         keymap = $3,
		       pronouns = $4,
		    referrer_id = (SELECT id FROM users WHERE login = $5 AND id != $9),
		   show_country = $6,
		          theme = $7,
		      time_zone = $8
		  WHERE id = $9`,
		r.Form.Get("country"),
		r.Form.Get("layout"),
		r.Form.Get("keymap"),
		null.New(r.Form.Get("pronouns"), r.Form.Get("pronouns") != ""),
		r.Form.Get("referrer"),
		r.Form.Get("show_country") == "on",
		r.Form.Get("theme"),
		r.Form.Get("time_zone"),
		userID,
	)

	// Update connections' publicness if they differ from DB.
	for _, c := range oauth.GetConnections(tx, userID, false) {
		if show := r.Form.Get("show_"+c.Connection) == "on"; show != c.Public {
			tx.MustExec(
				`UPDATE connections
				    SET public = $1
				  WHERE connection = $2 AND user_id = $3`,
				show,
				c.Connection,
				userID,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		panic(err)
	}

	http.Redirect(w, r, "/golfer/settings", http.StatusSeeOther)
}
