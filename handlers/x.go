package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"zgo.at/blackmail"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/bgrun"
	"zgo.at/guru"
	"zgo.at/zhttp"
	"zgo.at/zlog"
	"zgo.at/zvalidate"
)

func (h settings) users(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		var users goatcounter.Users
		err := users.List(r.Context(), Site(r.Context()).ID)
		if err != nil {
			return err
		}

		var sites goatcounter.Sites
		err = sites.ForThisAccount(r.Context(), false)
		if err != nil {
			return err
		}

		return zhttp.Template(w, "settings_users.gohtml", struct {
			Globals
			Users    goatcounter.Users
			Sites    goatcounter.Sites
			Validate *zvalidate.Validator
		}{newGlobals(w, r), users, sites, verr})
	}
}

func (h settings) usersAdd(w http.ResponseWriter, r *http.Request) error {
	var args struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	mainSite := Site(r.Context())
	err = mainSite.GetMain(r.Context())
	if err != nil {
		return err
	}

	newUser := goatcounter.User{
		Email: args.Email,
		Site:  mainSite.ID,
	}
	if args.Password == "" {
		newUser.Password = []byte(args.Password)
	}
	if !goatcounter.Config(r.Context()).GoatcounterCom {
		newUser.EmailVerified = true
	}

	err = newUser.Insert(r.Context(), true)
	if err != nil {
		return err
	}
	if err != nil {
		zhttp.FlashError(w, err.Error())
		return zhttp.SeeOther(w, "/settings/users")
	}

	ctx := goatcounter.CopyContextValues(r.Context())
	bgrun.Run(fmt.Sprintf("adduser:%d", newUser.ID), func() {
		err := blackmail.Send(fmt.Sprintf("A GoatCounter account was created for you at %s", mainSite.Display(ctx)),
			blackmail.From("GoatCounter", goatcounter.Config(r.Context()).EmailFrom),
			blackmail.To(newUser.Email),
			blackmail.BodyMustText(goatcounter.TplEmailAddUser{ctx, *mainSite, newUser, goatcounter.GetUser(ctx).Email}.Render),
		)
		if err != nil {
			zlog.Errorf(": %s", err)
		}
	})

	zhttp.Flash(w, "User ‘%s’ added.", newUser.Email)
	return zhttp.SeeOther(w, "/settings/users")
}

func (h settings) usersRemove(w http.ResponseWriter, r *http.Request) error {
	v := zvalidate.New()
	id := v.Integer("id", chi.URLParam(r, "id"))
	if v.HasErrors() {
		return v
	}

	mainSite := Site(r.Context())
	err := mainSite.GetMain(r.Context())
	if err != nil {
		return err
	}

	var user goatcounter.User
	err = user.ByID(r.Context(), id)
	if err != nil {
		return err
	}

	if user.Site != mainSite.ID {
		return guru.New(404, "Not Found")
	}

	err = user.Delete(r.Context())
	if err != nil {
		return err
	}

	zhttp.Flash(w, "User ‘%s’ removed.", user.Email)
	return zhttp.SeeOther(w, "/settings/users")
}
