// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package handlers

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"time"

	"code.soquee.net/otp"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/xsrftoken"
	"zgo.at/blackmail"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/bgrun"
	"zgo.at/guru"
	"zgo.at/zdb"
	"zgo.at/zhttp"
	"zgo.at/zhttp/auth"
	"zgo.at/zhttp/mware"
	"zgo.at/zlog"
	"zgo.at/zvalidate"
)

const (
	actionTOTP = "totp"
	mfaError   = "Token did not match; perhaps you waited too long? Try again."
)

type user struct{}

func (h user) mount(r chi.Router) {
	r.Get("/user/new", zhttp.Wrap(h.login))
	r.Get("/user/forgot", zhttp.Wrap(h.forgot))
	r.Post("/user/request-reset", zhttp.Wrap(h.requestReset))

	// Rate limit login attempts.
	rate := r.With(mware.Ratelimit(mware.RatelimitOptions{
		Client: mware.RatelimitIP,
		Store:  mware.NewRatelimitMemory(),
		Limit:  mware.RatelimitLimit(20, 60),
	}))
	rate.Post("/user/requestlogin", zhttp.Wrap(h.requestLogin))
	rate.Post("/user/totplogin", zhttp.Wrap(h.totpLogin))
	rate.Get("/user/reset/{key}", zhttp.Wrap(h.reset))
	rate.Get("/user/verify/{key}", zhttp.Wrap(h.verify))
	rate.Post("/user/reset/{key}", zhttp.Wrap(h.doReset))

	auth := r.With(loggedIn)
	auth.Post("/user/logout", zhttp.Wrap(h.logout))
	auth.Post("/user/change-password", zhttp.Wrap(h.changePassword))
	auth.Post("/user/disable-totp", zhttp.Wrap(h.disableTOTP))
	auth.Post("/user/enable-totp", zhttp.Wrap(h.enableTOTP))
	auth.Post("/user/resend-verify", zhttp.Wrap(h.resendVerify))
	auth.Post("/user/api-token", zhttp.Wrap(h.newAPIToken))
	auth.Post("/user/api-token/remove/{id}", zhttp.Wrap(h.deleteAPIToken))
}

func (h user) login(w http.ResponseWriter, r *http.Request) error {
	u := goatcounter.GetUser(r.Context())
	if u != nil && u.ID > 0 {
		return zhttp.SeeOther(w, "/")
	}

	return zhttp.Template(w, "user.gohtml", struct {
		Globals
		Email string
	}{newGlobals(w, r), r.URL.Query().Get("email")})
}

func (h user) forgot(w http.ResponseWriter, r *http.Request) error {
	u := goatcounter.GetUser(r.Context())
	if u != nil && u.ID > 0 {
		return zhttp.SeeOther(w, "/")
	}

	return zhttp.Template(w, "user_forgot_pw.gohtml", struct {
		Globals
		Email    string
		Page     string
		MetaDesc string
	}{newGlobals(w, r), r.URL.Query().Get("email"), "forgot_pw", "Forgot password – GoatCounter"})
}

func (h user) requestReset(w http.ResponseWriter, r *http.Request) error {
	u := goatcounter.GetUser(r.Context())
	if u != nil && u.ID > 0 {
		return zhttp.SeeOther(w, "/")
	}

	// Legacy email flow.
	args := struct {
		Email string `json:"email"`
	}{}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	err = u.ByEmail(r.Context(), args.Email)
	if err != nil {
		if zdb.ErrNoRows(err) {
			zhttp.FlashError(w, "Not an account on this site: %q", args.Email)
			return zhttp.SeeOther(w, fmt.Sprintf("/user/new?email=%s", url.QueryEscape(args.Email)))
		}
		return err
	}

	err = u.RequestReset(r.Context())
	if err != nil {
		return err
	}

	site := Site(r.Context())
	ctx := goatcounter.CopyContextValues(r.Context())
	bgrun.Run("email:password", func() {
		err := blackmail.Send(
			fmt.Sprintf("Password reset for %s", site.Domain(ctx)),
			blackmail.From("GoatCounter login", goatcounter.Config(ctx).EmailFrom),
			blackmail.To(u.Email),
			blackmail.BodyMustText(goatcounter.TplEmailPasswordReset{ctx, *site, *u}.Render))
		if err != nil {
			zlog.Errorf("password reset: %s", err)
		}
	})

	zhttp.Flash(w, "Email sent to %q", args.Email)
	return zhttp.SeeOther(w, "/user/forgot")
}

func (h user) totpLogin(w http.ResponseWriter, r *http.Request) error {
	args := struct {
		LoginMAC string `json:"loginmac"`
		Token    string `json:"totp_token"`
	}{}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	site := Site(r.Context())
	u := goatcounter.GetUser(r.Context())

	valid := xsrftoken.Valid(args.LoginMAC, *u.LoginToken, strconv.FormatInt(u.ID, 10), actionTOTP)
	if !valid {
		zhttp.Flash(w, "Invalid login")
		return zhttp.SeeOther(w, "/")
	}

	tokGen := otp.NewOTP(u.TOTPSecret, 6, sha1.New, otp.TOTP(30*time.Second, time.Now))
	tokInt, err := strconv.ParseInt(args.Token, 10, 32)
	if err != nil {
		return err
	}

	// Check a 30 second window on either side of the current time as well. It's
	// common for clocks to be slightly out of sync and this prevents most errors
	// and is what the spec recommends.
	if tokGen(0, nil) != int32(tokInt) &&
		tokGen(-1, nil) != int32(tokInt) &&
		tokGen(1, nil) != int32(tokInt) {
		zhttp.FlashError(w, mfaError)
		return zhttp.Template(w, "totp.gohtml", struct {
			Globals
			LoginMAC string
		}{newGlobals(w, r), args.LoginMAC})
	}

	auth.SetCookie(w, *u.LoginToken, cookieDomain(site, r))
	return zhttp.SeeOther(w, "/")
}

func (h user) requestLogin(w http.ResponseWriter, r *http.Request) error {
	u := goatcounter.GetUser(r.Context())
	if u != nil && u.ID > 0 { // Already logged in.
		return zhttp.SeeOther(w, "/")
	}

	args := struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}{}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	var user goatcounter.User
	err = user.ByEmail(r.Context(), args.Email)
	if err != nil {
		if zdb.ErrNoRows(err) {
			zhttp.FlashError(w, "User %q not found", args.Email)
			return zhttp.SeeOther(w, "/user/new")
		}
		return err
	}

	if user.Password == nil {
		zhttp.FlashError(w,
			"There is no password set for %q; please reset it",
			args.Email)
		return zhttp.SeeOther(w, "/user/forgot?email="+url.QueryEscape(args.Email))
	}

	err = bcrypt.CompareHashAndPassword(user.Password, []byte(args.Password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			zhttp.FlashError(w, "Wrong password for %q", args.Email)
		} else {
			zhttp.FlashError(w, "Something went wrong :-( An error has been logged for investigation.")
			zlog.Error(err)
		}
		return zhttp.SeeOther(w, "/user/new?email="+url.QueryEscape(args.Email))
	}

	err = user.Login(r.Context())
	if err != nil {
		return err
	}

	if user.TOTPEnabled {
		return zhttp.Template(w, "totp.gohtml", struct {
			Globals
			LoginMAC string
		}{newGlobals(w, r), xsrftoken.Generate(*user.LoginToken, strconv.FormatInt(user.ID, 10), actionTOTP)})
	}

	auth.SetCookie(w, *user.LoginToken, cookieDomain(Site(r.Context()), r))
	return zhttp.SeeOther(w, "/")
}

func (h user) reset(w http.ResponseWriter, r *http.Request) error {
	site := Site(r.Context())
	key := chi.URLParam(r, "key")

	var user goatcounter.User
	err := user.ByResetToken(r.Context(), key)
	if err != nil {
		if !zdb.ErrNoRows(err) {
			zlog.Error(err)
		}
		return guru.New(http.StatusForbidden, "could find the user for the given token; perhaps it's expired or has already been used?")
	}

	user.ID = 0 // Don't count as logged in.

	return zhttp.Template(w, "user_reset.gohtml", struct {
		Globals
		Site *goatcounter.Site
		User goatcounter.User
		Key  string
	}{newGlobals(w, r), site, user, key})
}

func (h user) doReset(w http.ResponseWriter, r *http.Request) error {
	var user goatcounter.User
	err := user.ByResetToken(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		return guru.New(http.StatusForbidden, "could find the user for the given token; perhaps it's expired or has already been used?")
	}

	var args struct {
		Password  string `json:"password"`
		Password2 string `json:"password2"`
	}
	_, err = zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	if args.Password != args.Password2 {
		zhttp.FlashError(w, "Password confirmation doesn’t match.")
		return zhttp.SeeOther(w, "/user/new")
	}

	err = user.UpdatePassword(r.Context(), args.Password)
	if err != nil {
		var vErr *zvalidate.Validator
		if errors.As(err, &vErr) {
			zhttp.FlashError(w, fmt.Sprintf("%s", err))
			return zhttp.SeeOther(w, "/user/new")
		}
		return err
	}

	zhttp.Flash(w, "Password reset; use your new password to login.")
	return zhttp.SeeOther(w, "/user/new")
}

func (h user) logout(w http.ResponseWriter, r *http.Request) error {
	if goatcounter.Config(r.Context()).GoatcounterCom {
		isAdmin := false
		for _, c := range r.Cookies() {
			if c.Name == "is_admin" {
				isAdmin = true
				break
			}
		}
		if isAdmin {
			auth.ClearCookie(w, Site(r.Context()).Domain(r.Context()))
			return zhttp.SeeOther(w, "https://www.goatcounter.com")
		}
	}

	u := goatcounter.GetUser(r.Context())
	err := u.Logout(r.Context())
	if err != nil {
		zlog.Errorf("logout: %s", err)
	}

	auth.ClearCookie(w, Site(r.Context()).Domain(r.Context()))
	return zhttp.SeeOther(w, "/")
}

func (h user) disableTOTP(w http.ResponseWriter, r *http.Request) error {
	u := goatcounter.GetUser(r.Context())
	err := u.DisableTOTP(r.Context())
	if err != nil {
		return err
	}

	return zhttp.SeeOther(w, "/settings/auth")
}

func (h user) enableTOTP(w http.ResponseWriter, r *http.Request) error {
	u := goatcounter.GetUser(r.Context())
	var args struct {
		Token string `json:"totp_token"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	tokGen := otp.NewOTP(u.TOTPSecret, 6, sha1.New, otp.TOTP(30*time.Second, time.Now))
	tokInt, err := strconv.ParseInt(args.Token, 10, 32)
	if err != nil {
		return err
	}

	// Check a 30 second window on either side of the current time as well. It's
	// common for clocks to be slightly out of sync and this prevents most errors
	// and is what the spec recommends.
	if tokGen(0, nil) != int32(tokInt) &&
		tokGen(-1, nil) != int32(tokInt) &&
		tokGen(1, nil) != int32(tokInt) {
		zhttp.FlashError(w, mfaError)
		return zhttp.SeeOther(w, "/settings/auth")
	}

	err = u.EnableTOTP(r.Context())
	if err != nil {
		return err
	}
	return zhttp.SeeOther(w, "/settings/auth")
}

func (h user) changePassword(w http.ResponseWriter, r *http.Request) error {
	u := goatcounter.GetUser(r.Context())
	var args struct {
		CPassword string `json:"c_password"`
		Password  string `json:"password"`
		Password2 string `json:"password2"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	if len(u.Password) > 0 {
		ok, err := u.CorrectPassword(args.CPassword)
		if err != nil {
			return err
		}
		if !ok {
			zhttp.FlashError(w, "Current password is incorrect.")
			return zhttp.SeeOther(w, "/settings/auth")
		}
	}

	if args.Password != args.Password2 {
		zhttp.FlashError(w, "Password confirmation doesn’t match.")
		return zhttp.SeeOther(w, "/settings/auth")
	}

	err = u.UpdatePassword(r.Context(), args.Password)
	if err != nil {
		var vErr *zvalidate.Validator
		if errors.As(err, &vErr) {
			zhttp.FlashError(w, fmt.Sprintf("%s", err))
			return zhttp.SeeOther(w, "/settings/auth")
		}
		return err
	}

	zhttp.Flash(w, "Password changed")
	return zhttp.SeeOther(w, "/")
}

func (h user) resendVerify(w http.ResponseWriter, r *http.Request) error {
	user := goatcounter.GetUser(r.Context())
	if user.EmailVerified {
		zhttp.Flash(w, "%q is already verified", user.Email)
		return zhttp.SeeOther(w, "/")
	}

	sendEmailVerify(r.Context(), Site(r.Context()), user, goatcounter.Config(r.Context()).EmailFrom)
	zhttp.Flash(w, "Sent to %q", user.Email)
	return zhttp.SeeOther(w, "/")
}

func (h user) newAPIToken(w http.ResponseWriter, r *http.Request) error {
	user := goatcounter.GetUser(r.Context())
	if !user.EmailVerified {
		zhttp.Flash(w, "need to verify your email before you can use the API")
		return zhttp.SeeOther(w, "/settings/auth")
	}

	var token goatcounter.APIToken
	_, err := zhttp.Decode(r, &token)
	if err != nil {
		return err
	}

	err = token.Insert(r.Context())
	if err != nil {
		return err
	}

	zhttp.Flash(w, "Token created")
	return zhttp.SeeOther(w, "/settings/auth")
}

func (h user) deleteAPIToken(w http.ResponseWriter, r *http.Request) error {
	v := zvalidate.New()
	id := v.Integer("id", chi.URLParam(r, "id"))
	if v.HasErrors() {
		return v
	}

	var token goatcounter.APIToken
	err := token.ByID(r.Context(), id)
	if err != nil {
		return err
	}

	err = token.Delete(r.Context())
	if err != nil {
		return err
	}

	zhttp.Flash(w, "Token removed")
	return zhttp.SeeOther(w, "/settings/auth")
}

func sendEmailVerify(ctx context.Context, site *goatcounter.Site, user *goatcounter.User, emailFrom string) {
	ctx = goatcounter.CopyContextValues(ctx)
	bgrun.Run("email:verify", func() {
		err := blackmail.Send("Verify your email",
			mail.Address{Name: "GoatCounter", Address: emailFrom},
			blackmail.To(user.Email),
			blackmail.BodyMustText(goatcounter.TplEmailVerify{ctx, *site, *user}.Render))
		if err != nil {
			zlog.Errorf("blackmail: %s", err)
		}
	})
}

func (h user) verify(w http.ResponseWriter, r *http.Request) error {
	key := chi.URLParam(r, "key")
	var user goatcounter.User
	err := user.ByEmailToken(r.Context(), key)
	if err != nil {
		if zdb.ErrNoRows(err) {
			return guru.New(400, "unknown token; perhaps it was already used?")
		}
		return err
	}

	if user.EmailVerified {
		zhttp.Flash(w, "%q is already verified", user.Email)
		return zhttp.SeeOther(w, "/")
	}

	if key != *user.EmailToken {
		zhttp.FlashError(w, "Wrong verification key")
		return zhttp.SeeOther(w, "/")
	}

	err = user.VerifyEmail(r.Context())
	if err != nil {
		return err
	}

	zhttp.Flash(w, "%q verified", user.Email)
	return zhttp.SeeOther(w, "/")
}

// Make sure to use the currect cookie, since both "custom.example.com" and
// "example.goatcounter.com" will work if you're using a custom domain.
func cookieDomain(site *goatcounter.Site, r *http.Request) string {
	if r.Host == site.Domain(r.Context()) {
		return site.Domain(r.Context())
	}
	return goatcounter.Config(r.Context()).Domain
}
