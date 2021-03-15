// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package handlers

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/monoculum/formam"
	"zgo.at/blackmail"
	"zgo.at/errors"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/acme"
	"zgo.at/goatcounter/bgrun"
	"zgo.at/goatcounter/widgets"
	"zgo.at/guru"
	"zgo.at/tz"
	"zgo.at/zdb"
	"zgo.at/zhttp"
	"zgo.at/zhttp/header"
	"zgo.at/zhttp/mware"
	"zgo.at/zlog"
	"zgo.at/zstd/zcrypto"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/zstring"
	"zgo.at/zvalidate"
)

type settings struct{}

func (h settings) mount(r chi.Router) {
	r.Get("/settings", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zhttp.SeeOther(w, "/settings/main")
	}))

	r.Get("/settings/main", zhttp.Wrap(h.main(nil)))
	r.Post("/settings/main", zhttp.Wrap(h.mainSave))
	r.Get("/settings/main/ip", zhttp.Wrap(h.ip))
	r.Get("/settings/change-code", zhttp.Wrap(h.changeCode))
	r.Post("/settings/change-code", zhttp.Wrap(h.changeCode))

	r.Get("/settings/dashboard", zhttp.Wrap(h.dashboard(nil)))
	r.Post("/settings/dashboard", zhttp.Wrap(h.dashboardSave))

	r.Get("/settings/sites", zhttp.Wrap(h.sites(nil)))
	r.Post("/settings/sites/add", zhttp.Wrap(h.sitesAdd))
	r.Get("/settings/sites/remove/{id}", zhttp.Wrap(h.sitesRemoveConfirm))
	r.Post("/settings/sites/remove/{id}", zhttp.Wrap(h.sitesRemove))
	r.Post("/settings/sites/copySettings", zhttp.Wrap(h.sitesCopySettings))

	r.Get("/settings/users", zhttp.Wrap(h.users(nil)))
	r.Post("/settings/users/add", zhttp.Wrap(h.usersAdd))
	r.Post("/settings/users/remove/{id}", zhttp.Wrap(h.usersRemove))

	r.Get("/settings/purge", zhttp.Wrap(h.purge(nil)))
	r.Get("/settings/purge/confirm", zhttp.Wrap(h.purgeConfirm))
	r.Post("/settings/purge", zhttp.Wrap(h.purgeDo))

	r.Get("/settings/export", zhttp.Wrap(h.export(nil)))
	r.Get("/settings/export/{id}", zhttp.Wrap(h.exportDownload))
	r.Post("/settings/export/import", zhttp.Wrap(h.exportImport))
	r.With(mware.Ratelimit(mware.RatelimitOptions{
		Client:  mware.RatelimitIP,
		Store:   mware.NewRatelimitMemory(),
		Limit:   mware.RatelimitLimit(1, 3600),
		Message: "you can request only one export per hour",
	})).Post("/settings/export", zhttp.Wrap(h.exportStart))

	r.Get("/settings/auth", zhttp.Wrap(h.auth(nil)))

	r.Get("/settings/delete", zhttp.Wrap(h.delete(nil)))
	r.Post("/settings/delete", zhttp.Wrap(h.deleteDo))

	r.Post("/settings/view", zhttp.Wrap(h.viewSave))
}

func (h settings) main(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		return zhttp.Template(w, "settings_main.gohtml", struct {
			Globals
			Validate  *zvalidate.Validator
			Timezones []*tz.Zone
		}{newGlobals(w, r), verr, tz.Zones})
	}
}

func (h settings) mainSave(w http.ResponseWriter, r *http.Request) error {
	v := zvalidate.New()

	args := struct {
		Cname      string                   `json:"cname"`
		LinkDomain string                   `json:"link_domain"`
		Settings   goatcounter.SiteSettings `json:"settings"`
		User       goatcounter.User         `json:"user"`
	}{}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		ferr, ok := err.(*formam.Error)
		if !ok || ferr.Code() != formam.ErrCodeConversion {
			return err
		}
		v.Append(ferr.Path(), "must be a number")

		// TODO: we return here because formam stops decoding on the first
		// error. We should really fix this in formam, but it's an incompatible
		// change.
		return h.main(&v)(w, r)
	}

	txctx, tx, err := zdb.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback()

	user := goatcounter.GetUser(txctx)

	emailChanged := false
	if goatcounter.Config(r.Context()).GoatcounterCom && args.User.Email != user.Email {
		emailChanged = true
	}

	user.Email = args.User.Email
	err = user.Update(txctx, emailChanged)
	if err != nil {
		var vErr *zvalidate.Validator
		if !errors.As(err, &vErr) {
			return err
		}
		v.Sub("user", "", err)
	}

	site := Site(txctx)
	site.Settings = args.Settings
	site.LinkDomain = args.LinkDomain
	if args.Cname != "" && !site.PlanCustomDomain(txctx) {
		return guru.New(http.StatusForbidden, "need a business plan to set custom domain")
	}

	makecert := false
	if args.Cname == "" {
		site.Cname = nil
	} else {
		if site.Cname == nil || *site.Cname != args.Cname {
			makecert = true // Make after we persisted to DB.
		}
		site.Cname = &args.Cname
	}

	err = site.Update(txctx)
	if err != nil {
		var vErr *zvalidate.Validator
		if !errors.As(err, &vErr) {
			return err
		}
		v.Sub("site", "", err)
	}

	if v.HasErrors() {
		return h.main(&v)(w, r)
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	if emailChanged {
		sendEmailVerify(r.Context(), site, user, goatcounter.Config(r.Context()).EmailFrom)
	}

	if makecert {
		ctx := goatcounter.CopyContextValues(r.Context())
		bgrun.Run(fmt.Sprintf("acme.Make:%s", args.Cname), func() {
			err := acme.Make(ctx, args.Cname)
			if err != nil {
				zlog.Field("domain", args.Cname).Error(err)
				return
			}

			err = site.UpdateCnameSetupAt(ctx)
			if err != nil {
				zlog.Field("domain", args.Cname).Error(err)
			}
		})
	}

	zhttp.Flash(w, "Saved!")
	return zhttp.SeeOther(w, "/settings")
}

func (h settings) changeCode(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		return zhttp.Template(w, "settings_changecode.gohtml", struct {
			Globals
		}{newGlobals(w, r)})
	}

	var args struct {
		Code string `json:"code"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	site := Site(r.Context())
	err = site.UpdateCode(r.Context(), args.Code)
	if err != nil {
		return err
	}

	zhttp.Flash(w, "Saved!")
	return zhttp.SeeOther(w, site.URL(r.Context())+"/settings/main")
}

func (h settings) ip(w http.ResponseWriter, r *http.Request) error {
	return zhttp.String(w, r.RemoteAddr)
}

func (h settings) dashboard(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		return zhttp.Template(w, "settings_dashboard.gohtml", struct {
			Globals
			Validate *zvalidate.Validator
			Widgets  widgets.List
		}{newGlobals(w, r), verr,
			widgets.FromSiteWidgets(Site(r.Context()).Settings.Widgets, widgets.FilterInternal),
		})
	}
}

func (h settings) dashboardSave(w http.ResponseWriter, r *http.Request) error {
	err := r.ParseForm()
	if err != nil {
		return err
	}

	site := Site(r.Context())

	if r.Form.Get("reset") != "" {
		site.Settings.Widgets = nil
		site.Defaults(r.Context())
		err = site.Update(r.Context())
		if err != nil {
			return err
		}

		zhttp.Flash(w, "Reset to defaults!")
		return zhttp.SeeOther(w, "/settings/dashboard")
	}

	parse := make(map[string]map[string]string)
	for k, v := range r.Form {
		if !strings.HasPrefix(k, "widgets.") {
			continue
		}
		k = k[8:]

		name, key := zstring.Split2(k, ".")
		if parse[name] == nil {
			parse[name] = make(map[string]string)
		}
		parse[name][key] = v[0]
	}

	site.Settings.Widgets = make(goatcounter.Widgets, len(parse))
	for k, v := range parse {
		pos, err := strconv.Atoi(v["index"])
		if err != nil {
			return err
		}

		w := goatcounter.Widget{"name": k, "on": v["on"] == "on"}
		for sName, sVal := range v {
			if strings.HasPrefix(sName, "s.") {
				err := w.SetSetting(k, sName[2:], sVal)
				if err != nil {
					return err
				}
			}
		}
		site.Settings.Widgets[pos] = w
	}

	err = site.Update(r.Context())
	if err != nil {
		var v *zvalidate.Validator
		if errors.As(err, &v) {
			return h.dashboard(v)(w, r)
		}
		return err
	}

	zhttp.Flash(w, "Saved!")
	return zhttp.SeeOther(w, "/settings/dashboard")
}

func (h settings) sites(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		var sites goatcounter.Sites
		err := sites.ForThisAccount(r.Context(), false)
		if err != nil {
			return err
		}

		return zhttp.Template(w, "settings_sites.gohtml", struct {
			Globals
			SubSites goatcounter.Sites
			Validate *zvalidate.Validator
		}{newGlobals(w, r), sites, verr})
	}
}

func (h settings) sitesAdd(w http.ResponseWriter, r *http.Request) error {
	var args struct {
		Code  string `json:"code"`
		Cname string `json:"cname"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	mainSite, err := MainSite(r.Context())
	if err != nil {
		return err
	}

	var (
		newSite goatcounter.Site
		addr    = args.Code
	)
	if goatcounter.Config(r.Context()).GoatcounterCom {
		newSite.Code = args.Code
	} else {
		newSite.Code = "serve-" + zcrypto.Secret64()
		newSite.Cname = &args.Cname
		addr = args.Cname
	}

	// Undelete previous soft-deleted site.
	id, err := newSite.Exists(r.Context())
	if err != nil {
		return err
	}
	if id > 0 {
		err := newSite.ByIDState(r.Context(), id, goatcounter.StateDeleted)
		if err != nil {
			if zdb.ErrNoRows(err) {
				return guru.Errorf(400, "%q already exists", addr)
			}
			return err
		}
		if newSite.Parent == nil || *newSite.Parent != mainSite.ID {
			return guru.Errorf(400, "%q already exists", addr)
		}

		err = newSite.Undelete(r.Context(), newSite.ID)
		if err != nil {
			return err
		}

		zhttp.Flash(w, "Site ‘%s’ was previously deleted; restored site with all data.", newSite.URL(r.Context()))
		return zhttp.SeeOther(w, "/settings/sites")
	}

	// Create new site.
	newSite.Parent = &mainSite.ID
	newSite.Plan = goatcounter.PlanChild
	newSite.Settings = Site(r.Context()).Settings
	err = zdb.TX(r.Context(), func(ctx context.Context) error {
		err = newSite.Insert(ctx)
		if err != nil {
			return err
		}
		if !goatcounter.Config(r.Context()).GoatcounterCom {
			return newSite.UpdateCnameSetupAt(ctx)
		}
		return nil
	})
	if err != nil {
		zhttp.FlashError(w, err.Error())
		return zhttp.SeeOther(w, "/settings/sites")
	}

	zhttp.Flash(w, "Site ‘%s’ added.", newSite.URL(r.Context()))
	return zhttp.SeeOther(w, "/settings/sites")
}

func (h settings) sitesRemoveConfirm(w http.ResponseWriter, r *http.Request) error {
	v := zvalidate.New()
	id := v.Integer("id", chi.URLParam(r, "id"))
	if v.HasErrors() {
		return v
	}

	var s goatcounter.Site
	err := s.ByID(r.Context(), id)
	if err != nil {
		return err
	}

	return zhttp.Template(w, "settings_sites_rm_confirm.gohtml", struct {
		Globals
		Rm goatcounter.Site
	}{newGlobals(w, r), s})
}

func (h settings) sitesRemove(w http.ResponseWriter, r *http.Request) error {
	v := zvalidate.New()
	id := v.Integer("id", chi.URLParam(r, "id"))
	if v.HasErrors() {
		return v
	}

	var s goatcounter.Site
	err := s.ByID(r.Context(), id)
	if err != nil {
		return err
	}

	site := Site(r.Context())
	if !(s.ID == site.ID || (s.Parent != nil && *s.Parent == site.ID)) {
		return guru.New(404, "Not Found")
	}

	sID := s.ID
	err = s.Delete(r.Context(), false)
	if err != nil {
		return err
	}

	zhttp.Flash(w, "Site ‘%s’ removed.", s.URL(r.Context()))

	// Redirect to parent if we're removing the current site.
	if sID == Site(r.Context()).ID && s.Parent != nil {
		var parent goatcounter.Site
		err = parent.ByID(r.Context(), *s.Parent)
		if err != nil {
			zlog.Error(err)
			return zhttp.SeeOther(w, "/")
		}
		return zhttp.SeeOther(w, parent.URL(r.Context()))
	}
	return zhttp.SeeOther(w, "/settings/sites")
}

func (h settings) sitesCopySettings(w http.ResponseWriter, r *http.Request) error {
	master := Site(r.Context())

	var args struct {
		Sites    []int64 `json:"sites"`
		AllSites bool    `json:"allsites"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	var copies goatcounter.Sites
	if args.AllSites {
		err := copies.ForThisAccount(r.Context(), true)
		if err != nil {
			return err
		}
	} else {
		for _, c := range args.Sites {
			var s goatcounter.Site
			err := s.ByID(r.Context(), c)
			if err != nil {
				return err
			}
			if s.Parent == nil || *s.Parent != master.ID {
				return guru.Errorf(http.StatusForbidden, "yeah nah, site %d doesn't belong to you", s.ID)
			}
			copies = append(copies, s)
		}
	}

	for _, c := range copies {
		c.Settings = master.Settings
		err := c.Update(r.Context())
		if err != nil {
			return err
		}
	}

	zhttp.Flash(w, "Settings copied to the selected sites.")
	return zhttp.SeeOther(w, "/settings/sites")
}

func (h settings) purge(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		return zhttp.Template(w, "settings_purge.gohtml", struct {
			Globals
			Validate *zvalidate.Validator
		}{newGlobals(w, r), verr})
	}
}

func (h settings) purgeConfirm(w http.ResponseWriter, r *http.Request) error {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	title := r.URL.Query().Get("match-title") == "on"

	var list goatcounter.HitLists
	err := list.ListPathsLike(r.Context(), path, title)
	if err != nil {
		return err
	}

	return zhttp.Template(w, "settings_purge_confirm.gohtml", struct {
		Globals
		PurgePath string
		List      goatcounter.HitLists
	}{newGlobals(w, r), path, list})
}

func (h settings) purgeDo(w http.ResponseWriter, r *http.Request) error {
	paths, err := zint.Split(r.Form.Get("paths"), ",")
	if err != nil {
		return err
	}

	ctx := goatcounter.CopyContextValues(r.Context())
	bgrun.Run(fmt.Sprintf("purge:%d", Site(ctx).ID), func() {
		var list goatcounter.Hits
		err := list.Purge(ctx, paths)
		if err != nil {
			zlog.Error(err)
		}
	})

	zhttp.Flash(w, "Started in the background; may take about 10-20 seconds to fully process.")
	return zhttp.SeeOther(w, "/settings/purge")
}

func (h settings) export(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		var exports goatcounter.Exports
		err := exports.List(r.Context())
		if err != nil {
			return err
		}

		return zhttp.Template(w, "settings_export.gohtml", struct {
			Globals
			Validate *zvalidate.Validator
			Exports  goatcounter.Exports
		}{newGlobals(w, r), verr, exports})
	}
}

func (h settings) exportDownload(w http.ResponseWriter, r *http.Request) error {
	v := zvalidate.New()
	id := v.Integer("id", chi.URLParam(r, "id"))
	if v.HasErrors() {
		return v
	}

	var export goatcounter.Export
	err := export.ByID(r.Context(), id)
	if err != nil {
		return err
	}

	fp, err := os.Open(export.Path)
	if err != nil {
		if os.IsNotExist(err) {
			zhttp.FlashError(w, "It looks like there is no export yet.")
			return zhttp.SeeOther(w, "/settings/export")
		}

		return err
	}
	defer fp.Close()

	err = header.SetContentDisposition(w.Header(), header.DispositionArgs{
		Type:     header.TypeAttachment,
		Filename: filepath.Base(export.Path),
	})
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/gzip")
	return zhttp.Stream(w, fp)
}

func (h settings) exportImport(w http.ResponseWriter, r *http.Request) error {
	v := zvalidate.New()
	replace := v.Boolean("replace", r.Form.Get("replace"))
	if v.HasErrors() {
		return v
	}

	file, head, err := r.FormFile("csv")
	if err != nil {
		return err
	}
	defer file.Close()

	var fp io.ReadCloser = file
	if strings.HasSuffix(head.Filename, ".gz") {
		fp, err = gzip.NewReader(file)
		if err != nil {
			return guru.Errorf(400, "could not read as gzip: %w", err)
		}
	}
	defer fp.Close()

	user := goatcounter.GetUser(r.Context())
	ctx := goatcounter.CopyContextValues(r.Context())
	n := 0
	bgrun.Run(fmt.Sprintf("import:%d", Site(ctx).ID), func() {
		firstHitAt, err := goatcounter.Import(ctx, fp, replace, true, func(hit goatcounter.Hit, final bool) {
			if final {
				return
			}

			goatcounter.Memstore.Append(hit)
			n++

			// Spread out the load a bit.
			if n%5000 == 0 {
				goatcounter.PersistRunner.Run <- struct{}{}
				for bgrun.Running("cron:PersistAndStat") {
					time.Sleep(250 * time.Millisecond)
				}
			}
		})
		if err != nil {
			if e, ok := err.(*errors.StackErr); ok {
				err = e.Unwrap()
			}

			sendErr := blackmail.Send("GoatCounter import error",
				blackmail.From("GoatCounter import", goatcounter.Config(r.Context()).EmailFrom),
				blackmail.To(user.Email),
				blackmail.BodyMustText(goatcounter.TplEmailImportError{err}.Render))
			if sendErr != nil {
				zlog.Error(sendErr)
			}
		}

		if firstHitAt != nil {
			err := Site(ctx).UpdateFirstHitAt(ctx, *firstHitAt)
			if err != nil {
				zlog.Error(err)
			}
		}
	})

	zhttp.Flash(w, "Import started in the background; you’ll get an email when it’s done.")
	return zhttp.SeeOther(w, "/settings/export")
}

func (h settings) exportStart(w http.ResponseWriter, r *http.Request) error {
	r.ParseForm()

	v := zvalidate.New()
	startFrom := v.Integer("startFrom", r.Form.Get("startFrom"))
	if v.HasErrors() {
		return v
	}

	var export goatcounter.Export
	fp, err := export.Create(r.Context(), startFrom)
	if err != nil {
		return err
	}

	ctx := goatcounter.CopyContextValues(r.Context())
	bgrun.Run(fmt.Sprintf("export web:%d", Site(ctx).ID),
		func() { export.Run(ctx, fp, true) })

	zhttp.Flash(w, "Export started in the background; you’ll get an email with a download link when it’s done.")
	return zhttp.SeeOther(w, "/settings/export")
}

func (h settings) auth(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		var tokens goatcounter.APITokens
		err := tokens.List(r.Context())
		if err != nil {
			return err
		}

		return zhttp.Template(w, "settings_auth.gohtml", struct {
			Globals
			Validate  *zvalidate.Validator
			APITokens goatcounter.APITokens
		}{newGlobals(w, r), verr, tokens})
	}
}

func (h settings) delete(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		del := map[string]interface{}{
			"ContactMe": r.URL.Query().Get("contact_me") == "true",
			"Reason":    r.URL.Query().Get("reason"),
		}

		var sites goatcounter.Sites
		err := sites.ForThisAccount(r.Context(), false)
		if err != nil {
			return err
		}

		return zhttp.Template(w, "settings_delete.gohtml", struct {
			Globals
			Sites    goatcounter.Sites
			Validate *zvalidate.Validator
			Delete   map[string]interface{}
		}{newGlobals(w, r), sites, verr, del})
	}
}

func (h settings) deleteDo(w http.ResponseWriter, r *http.Request) error {
	var args struct {
		Reason    string `json:"reason"`
		ContactMe bool   `json:"contact_me"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		zlog.Error(err)
	}

	mainSite, err := MainSite(r.Context())
	if err != nil {
		return err
	}

	has, err := hasPlan(r.Context(), mainSite)
	if err != nil {
		return err
	}
	if has {
		zhttp.FlashError(w, "This account still has a Stripe subscription; cancel that first on the billing page.")
		q := url.Values{}
		q.Set("reason", args.Reason)
		q.Set("contact_me", fmt.Sprintf("%t", args.ContactMe))
		return zhttp.SeeOther(w, "/settings/delete?"+q.Encode())
	}

	if args.Reason != "" {
		bgrun.Run("email:deletion", func() {
			contact := "false"
			if args.ContactMe {
				u := goatcounter.GetUser(r.Context())
				contact = u.Email
			}

			blackmail.Send("GoatCounter deletion",
				blackmail.From("GoatCounter deletion", goatcounter.Config(r.Context()).EmailFrom),
				blackmail.To(goatcounter.Config(r.Context()).EmailFrom),
				blackmail.Bodyf(`Deleted: %s (%d): contact_me: %s; reason: %s`,
					mainSite.Code, mainSite.ID, contact, args.Reason))
		})
	}

	err = mainSite.Delete(r.Context(), true)
	if err != nil {
		return err
	}
	return zhttp.SeeOther(w, "https://"+goatcounter.Config(r.Context()).Domain)
}

func (h settings) viewSave(w http.ResponseWriter, r *http.Request) error {
	site := Site(r.Context())
	v, i := site.Settings.Views.Get("default") // TODO: only default view for now.
	_, err := zhttp.Decode(r, &v)
	if err != nil {
		return err
	}

	site.Settings.Views[i] = v
	err = site.Update(r.Context())
	if err != nil {
		return err
	}

	return zhttp.JSON(w, map[string]string{})
}
