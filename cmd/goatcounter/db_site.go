// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package main

import (
	"context"
	"fmt"

	"zgo.at/errors"
	"zgo.at/goatcounter"
	"zgo.at/zdb"
	"zgo.at/zli"
	"zgo.at/zlog"
	"zgo.at/zstd/zcrypto"
	"zgo.at/zvalidate"
)

func cmdDBSite(f zli.Flags, cmd string, dbConnect, debug *string, createdb *bool) error {
	var db zdb.DB
	defer func() {
		if db != nil {
			db.Close()
		}
	}()

	parseFlag := func() (context.Context, error) {
		err := f.Parse()
		if err != nil {
			return nil, err
		}
		zlog.Config.SetDebug(*debug)

		db, _, err = connectDB(*dbConnect, []string{"pending"}, *createdb, false)
		if err != nil {
			return nil, err
		}

		ctx := goatcounter.NewContext(db)
		// Needs to be set, otherwise it will use the "code" logic to get the
		// domains.
		// TODO: in hindsight, storing it as "code" might not have been a good
		// idea, and just always setting "cname" would be better. Actually, that
		// column would have been better named "vhost".
		goatcounter.Config(ctx).Serve = true
		return ctx, nil
	}

	switch cmd {
	case "show":
		find := f.StringList(nil, "find")
		ctx, err := parseFlag()
		if err != nil {
			return err
		}

		sites, err := findSites(ctx, find.Strings())
		if err != nil {
			return err
		}

		ids := make([]int64, 0, len(sites))
		for _, s := range sites {
			ids = append(ids, s.ID)
		}
		zdb.Dump(ctx, zli.Stdout, `select * from sites where site_id in (?)`, ids, zdb.DumpVertical)

	case "create", "update":
		var (
			vhost = f.String("", "vhost")
			email = f.String("", "email")
			link  = f.String("", "link")
			pwd   = f.String("", "password")
			find  *[]string
		)
		if cmd == "update" {
			find = f.StringList(nil, "find").Pointer()
		}
		ctx, err := parseFlag()
		if err != nil {
			return err
		}

		if cmd == "create" {
			return cmdDBSiteCreate(ctx, vhost.String(), email.String(), link.String(), pwd.String())
		}
		return cmdDBSiteUpdate(ctx, *find, vhost, email, link, pwd)
	case "delete":
		var (
			find  = f.StringList(nil, "find").Pointer()
			hard  = f.Bool(false, "hard").Pointer()
			force = f.Bool(false, "force").Pointer()
		)
		ctx, err := parseFlag()
		if err != nil {
			return err
		}
		return cmdDBSiteDelete(ctx, *find, *hard, *force)
	}
	return nil
}

func cmdDBSiteCreate(ctx context.Context, vhost, email, link, pwd string) error {
	v := zvalidate.New()
	v.Required("-vhost", vhost)
	v.Domain("-vhost", vhost)
	if link == "" {
		v.Required("-email", email)
		v.Email("-email", email)
	}
	if v.HasErrors() {
		return v
	}

	err := (&goatcounter.Site{}).ByHost(ctx, vhost)
	if err == nil {
		return fmt.Errorf("there is already a site for the host %q", vhost)
	}

	var ps goatcounter.Site
	if link != "" {
		ps, err = findParent(ctx, link)
		if err != nil {
			return nil
		}
	}

	if pwd == "" {
		pwd, err = zli.AskPassword(8)
		if err != nil {
			return err
		}
	}

	return zdb.TX(ctx, func(ctx context.Context) error {
		s := goatcounter.Site{
			Code:  "serve-" + zcrypto.Secret64(),
			Cname: &vhost,
			Plan:  goatcounter.PlanBusinessPlus,
		}
		if ps.ID > 0 {
			s.Parent, s.Settings, s.Plan = &ps.ID, ps.Settings, goatcounter.PlanChild
		}
		err := s.Insert(ctx)
		if err != nil {
			return err
		}
		err = s.UpdateCnameSetupAt(ctx)
		if err != nil {
			return err
		}

		if link == "" { // Create user as well.
			err = (&goatcounter.User{Site: s.ID, Email: email, Password: []byte(pwd), EmailVerified: true}).Insert(ctx, false)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func cmdDBSiteUpdate(ctx context.Context, find []string,
	vhost, email, link, pwd interface {
		String() string
		Set() bool
	},
) error {

	v := zvalidate.New()
	v.Required("-find", find)
	v.Domain("-vhost", vhost.String())
	v.Email("-email", email.String())
	if v.HasErrors() {
		return v
	}

	sites, err := findSites(ctx, find)
	if err != nil {
		return err
	}

	return zdb.TX(ctx, func(ctx context.Context) error {
		for _, s := range sites {
			var u goatcounter.User
			// err = u.BySite(ctx, s.ID)
			if err != nil {
				return err
			}

			if vhost.Set() {
				p := vhost.String()
				s.Cname = &p
				err = s.Update(ctx)
				if err != nil {
					return err
				}
			}
			if link.Set() {
				ps, err := findParent(ctx, link.String())
				if err != nil {
					return err
				}
				err = s.UpdateParent(ctx, &ps.ID)
				if err != nil {
					return err
				}
			}

			if email.Set() {
				u.Email = email.String()
				err = u.Update(ctx, false)
				if err != nil {
					return err
				}
			}
			if pwd.Set() {
				err = u.UpdatePassword(ctx, pwd.String())
				if err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func cmdDBSiteDelete(ctx context.Context, find []string, hard, force bool) error {
	v := zvalidate.New()
	v.Required("-find", find)
	if v.HasErrors() {
		return v
	}

	sites, err := findSites(ctx, find)
	if err != nil {
		return err
	}

	return zdb.TX(ctx, func(ctx context.Context) error {
		for _, s := range sites {
			if hard {
				// TODO
			} else {
				err = s.Delete(ctx, force)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func findSites(ctx context.Context, find []string) (goatcounter.Sites, error) {
	var sites goatcounter.Sites
	for _, f := range find {
		var s goatcounter.Site
		err := s.Find(ctx, f)
		if err != nil {
			if zdb.ErrNoRows(err) {
				err = errors.Errorf("-find=%s: no site found", f)
			}
			return nil, err
		}
		sites = append(sites, s)
	}
	return sites, nil
}

func findParent(ctx context.Context, link string) (goatcounter.Site, error) {
	var s goatcounter.Site
	err := s.Find(ctx, link)
	if err != nil {
		return s, err
	}
	if s.Parent != nil {
		err = s.ByID(ctx, *s.Parent)
	}
	return s, err
}
