// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/mail"
	"os"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"zgo.at/blackmail"
	"zgo.at/errors"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/bgrun"
	"zgo.at/guru"
	"zgo.at/tz"
	"zgo.at/zdb"
	"zgo.at/zhttp"
	"zgo.at/zhttp/auth"
	"zgo.at/zhttp/header"
	"zgo.at/zhttp/mware"
	"zgo.at/zlog"
	"zgo.at/zstd/zfs"
	"zgo.at/zstripe"
	"zgo.at/zvalidate"
)

func NewWebsite(db zdb.DB, dev bool, defaultPlan string) chi.Router {
	r := chi.NewRouter()

	fsys, err := zfs.EmbedOrDir(goatcounter.Templates, "", dev)
	if err != nil {
		panic(err)
	}

	website{fsys, true, defaultPlan}.Mount(r, db, dev)
	return r
}

type website struct {
	templates   fs.FS
	fromWWW     bool
	defaultPlan string
}

func (h website) Mount(r chi.Router, db zdb.DB, dev bool) {
	r.Use(
		mware.RealIP(),
		mware.Unpanic(),
		middleware.RedirectSlashes,
		addctx(db, false),
		mware.WrapWriter(),
		mware.Headers(nil))
	if dev {
		mware.RequestLog(nil)
	}

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		zhttp.ErrPage(w, r, guru.New(404, "Not Found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		zhttp.ErrPage(w, r, guru.New(405, "Method Not Allowed"))
	})

	r.Get("/robots.txt", zhttp.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return zhttp.Text(w, "")
	}))
	r.Get("/ads.txt", zhttp.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return zhttp.Text(w, "")
	}))
	r.Get("/security.txt", zhttp.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return zhttp.Text(w, "Contact: support@goatcounter.com")
	}))

	h.MountShared(r)

	r.Get("/signup", zhttp.Wrap(h.signup))
	r.Post("/signup", zhttp.Wrap(h.doSignup))
	r.Get("/user/forgot", zhttp.Wrap(h.forgot))
	r.Post("/user/forgot", zhttp.Wrap(h.doForgot))
	for _, t := range []string{"", "privacy", "terms", "why", "data", "design"} {
		r.Get("/"+t, zhttp.Wrap(h.tpl))
	}

	r.With(mware.Ratelimit(mware.RatelimitOptions{
		Client:  mware.RatelimitIP,
		Store:   mware.NewRatelimitMemory(),
		Limit:   mware.RatelimitLimit(5, 86400),
		Message: "you can download this five times per day only",
	})).Get("/data/{file}", zhttp.Wrap(h.downloadData))
}

func (h website) MountShared(r chi.Router) {
	r.Get("/code", zhttp.Wrap(h.code))
	r.Get("/contribute", zhttp.Wrap(h.contribute))
	r.Get("/api.json", zhttp.Wrap(h.openAPI))
	r.Get("/api.html", zhttp.Wrap(h.openAPI))
	r.Get("/api2.html", zhttp.Wrap(h.openAPI))
	for _, t := range []string{"help", "gdpr", "api", "contact"} {
		r.Get("/"+t, zhttp.Wrap(h.tpl))
	}
}

var metaDesc = map[string]string{
	"":           "Simple web statistics. No tracking of personal data.",
	"help":       "Help and support – GoatCounter",
	"privacy":    "Privacy policy – GoatCounter",
	"gdpr":       "GDPR consent notices – GoatCounter",
	"terms":      "Terms of Service – GoatCounter",
	"contact":    "Contact – GoatCounter",
	"contribute": "Contribute – GoatCounter",
	"code":       "Site integration code – GoatCounter",
	"why":        "Why I made GoatCounter",
	"data":       "GoatCounter data",
	"api":        "API – GoatCounter",
	"design":     "GoatCounter design",
}

func (h website) openAPI(w http.ResponseWriter, r *http.Request) error {
	p := "tpl/" + path.Base(r.URL.Path)
	if p == "tpl/api.html" || p == "tpl/api2.html" {
		w.Header().Set("Content-Type", "text/html")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}

	fp, err := h.templates.Open(p)
	if err != nil {
		return guru.New(404, "Not Found")
	}
	defer fp.Close()

	d, err := io.ReadAll(fp)
	if err != nil {
		return err
	}

	if p == "tpl/api2.html" {
		url := "https://www.goatcounter.com"
		if s := goatcounter.GetSite(r.Context()); s != nil {
			url = s.URL(r.Context())
		}
		d = bytes.ReplaceAll(d, []byte(`spec-url=""`), []byte(fmt.Sprintf(`spec-url="%s/api.json"`, url)))
	}
	return zhttp.Bytes(w, d)
}

func (h website) tpl(w http.ResponseWriter, r *http.Request) error {
	t := path.Base(r.URL.Path[1:])
	if t == "" || t == "." {
		t = "home"
	}

	var loggedIn template.HTML
	if c, err := r.Cookie("key"); t == "home" && err == nil {
		var u goatcounter.User
		err = u.ByToken(r.Context(), c.Value)
		if err == nil {
			var s goatcounter.Site
			err = s.ByID(r.Context(), u.Site)
			if err == nil {
				loggedIn = template.HTML(fmt.Sprintf("Logged in as %s on <a href='%s'>%[2]s</a>",
					template.HTMLEscapeString(u.Email), template.HTMLEscapeString(s.URL(r.Context()))))
			}
		}
	}

	// if goatcounter.Config(r.Context()).Serve {
	// }
	return zhttp.Template(w, t+".gohtml", struct {
		Globals
		Page     string
		MetaDesc string
		LoggedIn template.HTML
	}{newGlobals(w, r), t, metaDesc[t], loggedIn})
}

func (h website) signup(w http.ResponseWriter, r *http.Request) error {
	return zhttp.Template(w, "signup.gohtml", struct {
		Globals
		Page       string
		MetaDesc   string
		Site       goatcounter.Site
		User       goatcounter.User
		Validate   *zvalidate.Validator
		TuringTest string
	}{newGlobals(w, r), "signup", "Sign up for GoatCounter", goatcounter.Site{},
		goatcounter.User{}, nil, ""})
}

type signupArgs struct {
	Code       string `json:"site_code"`
	LinkDomain string `json:"link_domain"`
	Timezone   string `json:"timezone"`
	Email      string `json:"user_email"`
	Password   string `json:"password"`
	TuringTest string `json:"turing_test"`
}

func (h website) doSignup(w http.ResponseWriter, r *http.Request) error {
	var args signupArgs
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	site := goatcounter.Site{Code: args.Code, LinkDomain: args.LinkDomain, Plan: h.defaultPlan}
	user := goatcounter.User{Email: args.Email, Password: []byte(args.Password)}

	v := zvalidate.New()
	if strings.TrimSpace(args.TuringTest) != "9" {
		v.Append("turing_test", "must fill in correct value")
		// Quick exit to prevent spurious errors/DB load from spambots.
		return zhttp.Template(w, "signup.gohtml", struct {
			Globals
			Page       string
			MetaDesc   string
			Site       goatcounter.Site
			User       goatcounter.User
			Validate   *zvalidate.Validator
			TuringTest string
		}{newGlobals(w, r), "signup", "Sign up for GoatCounter",
			site, user, &v, args.TuringTest})
	}

	txctx, tx, err := zdb.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Create site.
	lookup := (goatcounter.Location{}).LookupIP(r.Context(), r.RemoteAddr)
	var cc string
	if len(lookup) > 0 {
		cc = strings.Split(lookup, "-")[0]
	}
	tz, err := tz.New(cc, args.Timezone)
	if err != nil {
		zlog.FieldsRequest(r).Fields(zlog.F{
			"timezone": args.Timezone,
			"args":     fmt.Sprintf("%#v\n", args),
		}).Error(err)
	}
	site.Settings = goatcounter.SiteSettings{Timezone: tz}

	err = site.Insert(txctx)
	if err != nil {
		var vErr *zvalidate.Validator
		if !errors.As(err, &vErr) {
			return err
		}
		v.Sub("site", "", err)
	}

	// Create user.
	user.Site = site.ID
	err = user.Insert(txctx, false)
	if err != nil {
		var vErr *zvalidate.Validator
		if !errors.As(err, &vErr) {
			return err
		}
		v.Sub("user", "", err)
		delete(v.Errors, "user.site")
	}

	if v.HasErrors() {
		return zhttp.Template(w, "signup.gohtml", struct {
			Globals
			Page       string
			MetaDesc   string
			Site       goatcounter.Site
			User       goatcounter.User
			Validate   *zvalidate.Validator
			TuringTest string
		}{newGlobals(w, r), "signup", "Sign up for GoatCounter", site, user, &v, args.TuringTest})
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	err = user.Login(goatcounter.WithSite(r.Context(), &site))
	if err != nil {
		zlog.Errorf("login during account creation: %w", err)
	} else {
		auth.SetCookie(w, *user.LoginToken, cookieDomain(&site, r))
	}

	ctx := goatcounter.CopyContextValues(r.Context())
	bgrun.Run("welcome email", func() {
		err := blackmail.Send("Welcome to GoatCounter!",
			blackmail.From("GoatCounter", goatcounter.Config(r.Context()).EmailFrom),
			blackmail.To(user.Email),
			blackmail.BodyMustText(goatcounter.TplEmailWelcome{ctx, site, user, goatcounter.Config(ctx).DomainCount}.Render),
		)
		if err != nil {
			zlog.Errorf("welcome email: %s", err)
		}
	})

	return zhttp.SeeOther(w, fmt.Sprintf("%s/user/new", site.URL(r.Context())))
}

func (h website) forgot(w http.ResponseWriter, r *http.Request) error {
	return zhttp.Template(w, "user_forgot_code.gohtml", struct {
		Globals
		Page     string
		MetaDesc string
	}{newGlobals(w, r), "forgot", "Forgot domain – GoatCounter"})
}

func (h website) code(w http.ResponseWriter, r *http.Request) error {
	site := goatcounter.GetSite(r.Context())
	if site == nil {
		site = &goatcounter.Site{Code: "MYCODE"}
	}

	dc := goatcounter.Config(r.Context()).DomainCount
	if dc == "" {
		dc = Site(r.Context()).Domain(r.Context())
		port := goatcounter.Config(r.Context()).Port
		if port != "" {
			dc += port
		}
	}

	return zhttp.Template(w, "code.gohtml", struct {
		Globals
		Page        string
		MetaDesc    string
		CountDomain string
		SiteURL     string
		SiteDomain  string
		FromWWW     bool
	}{newGlobals(w, r), "code", "Site integration code – GoatCounter",
		dc, site.URL(r.Context()), site.Domain(r.Context()), h.fromWWW})
}

func (h website) downloadData(w http.ResponseWriter, r *http.Request) error {
	file := chi.URLParam(r, "file")
	if file == "" {
		return guru.New(400, "need file name")
	}
	switch file {
	case "ua.csv.gz", "bots.csv.gz", "screensize.csv.gz":
	default:
		return guru.Errorf(400, "unknown file name: %q", file)
	}

	fp, err := os.Open("/tmp/" + file)
	if err != nil {
		return err
	}
	defer fp.Close()

	err = header.SetContentDisposition(w.Header(), header.DispositionArgs{
		Type:     header.TypeAttachment,
		Filename: file,
	})
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/gzip")
	return zhttp.Stream(w, fp)
}

func (h website) doForgot(w http.ResponseWriter, r *http.Request) error {
	var args struct {
		Email string `json:"email"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	var users goatcounter.Users
	err = users.ByEmail(r.Context(), args.Email)
	if err != nil {
		return err
	}

	var sites goatcounter.Sites
	for _, u := range users {
		var s goatcounter.Site
		err := s.ByID(r.Context(), u.Site)
		if zdb.ErrNoRows(err) { // Deleted site.
			continue
		}
		if err != nil {
			return err
		}

		sites = append(sites, s)
	}

	ctx := goatcounter.CopyContextValues(r.Context())
	bgrun.Run("email:sites", func() {
		defer zlog.Recover()
		err := blackmail.Send("Your GoatCounter sites",
			mail.Address{Name: "GoatCounter", Address: goatcounter.Config(ctx).EmailFrom},
			blackmail.To(args.Email),
			blackmail.BodyMustText(goatcounter.TplEmailForgotSite{ctx, sites, args.Email}.Render))
		if err != nil {
			zlog.Error(err)
		}
	})

	zhttp.Flash(w, "List of login URLs mailed to %s", args.Email)
	return zhttp.SeeOther(w, "/user/forgot")
}

func (h website) contribute(w http.ResponseWriter, r *http.Request) error {
	return zhttp.Template(w, "contribute.gohtml", struct {
		Globals
		Page            string
		MetaDesc        string
		StripePublicKey string
		SKU             string
		FromWWW         bool
	}{newGlobals(w, r), "contribute", "Contribute – GoatCounter",
		zstripe.PublicKey, stripePlans[goatcounter.Config(r.Context()).Dev]["donate"], h.fromWWW})
}
