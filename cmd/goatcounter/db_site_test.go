// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"zgo.at/goatcounter"
	"zgo.at/goatcounter/gctest"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/zstring"
)

func TestDBSite(t *testing.T) {
	exit, _, out, ctx, dbc := startTest(t)

	// create
	{
		runCmd(t, exit, "db", "create", "site",
			"-db="+dbc,
			"-email=foo@foo.foo",
			"-vhost=stats.stats",
			"-password=password")
		wantExit(t, exit, out, 0)

		var s goatcounter.Site
		err := s.ByID(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		var u goatcounter.User
		err = u.BySite(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// create with parent
	{
		runCmd(t, exit, "db", "create", "site",
			"-db="+dbc,
			"-link=1",
			"-vhost=stats2.stats",
			"-password=password")
		wantExit(t, exit, out, 0)

		var s goatcounter.Site
		err := s.ByID(ctx, 3)
		if err != nil {
			t.Fatal(err)
		}
		if *s.Parent != 1 {
			t.Fatalf("s.Parent = %d", *s.Parent)
		}
		var u goatcounter.User
		err = u.BySite(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// show
	{
		runCmd(t, exit, "db", "show", "site",
			"-db="+dbc,
			"-find=1")
		wantExit(t, exit, out, 0)
		if !strings.HasPrefix(out.String(), `site_id         1`) {
			t.Error(out.String())
		}

		runCmd(t, exit, "db", "show", "site",
			"-db="+dbc,
			"-find=1", "-find=stats.stats")
		wantExit(t, exit, out, 0)
		if !strings.HasPrefix(out.String(), `site_id         1`) || !strings.Contains(out.String(), `site_id         2`) {
			t.Error(out.String())
		}
	}

	// update
	{
		_, site := gctest.Site(ctx, t, goatcounter.Site{})

		runCmd(t, exit, "db", "update", "site",
			"-db="+dbc,
			"-find=2",
			"-vhost=update.example.com",
			"-link="+strconv.FormatInt(site.ID, 10),
			"-email=update@example.com",
			"-password=newpassword",
		)
		wantExit(t, exit, out, 0)

		ctx = goatcounter.NewCache(ctx)

		var s goatcounter.Site
		err := s.ByID(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}

		var u goatcounter.User
		err = u.BySite(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}

		got := fmt.Sprintf("%s %s %s %s", zstring.Pointer{s.Cname}, zint.Pointer64{s.Parent}, u.Email, u.Password)
		want := `update.example.com 4 update@example.com $2a$04$5KGwHdizCIRgP8SUQFvY9OAWQONsa3zkSvKNup1xMTfEWGGw03mVi`
		if got != want {
			t.Errorf("\ngot:  %q\nwant: %q", got, want)
		}
	}
}
