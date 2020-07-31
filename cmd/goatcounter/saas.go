// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/cfg"
	"zgo.at/goatcounter/cron"
	"zgo.at/goatcounter/handlers"
	"zgo.at/zhttp"
	"zgo.at/zlog"
	"zgo.at/zstd/zstring"
	"zgo.at/zstripe"
	"zgo.at/zvalidate"
)

// saas
const usageSaas = `
This runs goatcounter.com

Running your own SaaS is currently undocumented, non-trivial, and has certain
assumptions that will not be true in your case. You do not want to run this; for
now it can only run run goatcounter.com

If you do want to run a SaaS, you're almost certainly better off writing your
own front-end to interface with GoatCounter (this is probably how
goatcounter.com should work as well, but it's quite some effort with low ROI to
change that now).

This command is undocumented on purpose. Get in touch if you think you need this
(but you probably don't) and we'll see what can be done to fix you up.
`

func saas() (int, error) {
	v := zvalidate.New()

	var stripe, domain, plan string
	CommandLine.StringVar(&domain, "domain", "goatcounter.localhost:8081,static.goatcounter.localhost:8081", "")
	CommandLine.StringVar(&stripe, "stripe", "", "")
	CommandLine.StringVar(&plan, "plan", goatcounter.PlanPersonal, "")
	CommandLine.StringVar(&cfg.Memcached, "memcached", "", "")

	dbConnect, test, dev, automigrate, listen, flagTLS, from, err := flagsServe(&v)
	if err != nil {
		return 1, err
	}

	goatcounter.InitCache()
	cron.InitCache()

	cfg.GoatcounterCom = true
	cfg.Plan = plan
	if flagTLS == "" {
		flagTLS = map[bool]string{true: "none", false: "acme"}[dev]
	}

	v.Include("-plan", plan, goatcounter.Plans)
	flagStripe(stripe, &v)
	flagDomain(domain, &v)
	flagFrom(from, &v)
	if cfg.Prod && cfg.Domain != "goatcounter.com" {
		v.Append("saas", "can only run on goatcounter.com")
	}

	if v.HasErrors() {
		return 1, v
	}

	db, tlsc, acmeh, listenTLS, err := setupServe(dbConnect, flagTLS, automigrate)
	if err != nil {
		return 2, err
	}

	// Set up HTTP handler and servers.
	d := zhttp.RemovePort(cfg.Domain)
	hosts := map[string]http.Handler{
		d:          zhttp.RedirectHost("//www." + cfg.Domain),
		"www." + d: handlers.NewWebsite(db),
		"*":        handlers.NewBackend(db, acmeh),
	}
	if dev {
		hosts[zhttp.RemovePort(cfg.DomainStatic)] = handlers.NewStatic(chi.NewRouter(), "./public", !dev)
	}

	doServe(db, test, listen, listenTLS, tlsc, hosts, func() {
		zlog.Printf("serving %q on %q; dev=%t", cfg.Domain, listen, dev)
	})
	return 0, nil
}

func banner() {
	fmt.Print(`
┏━━━━━━━━━━━━━━━━━━━━━ Thank you for using GoatCounter! ━━━━━━━━━━━━━━━━━━━━━━┓
┃                                                                             ┃
┃ Great you're choosing to self-host GoatCounter! I'd just like to put a      ┃
┃ reminder here that I work on this full-time; it's not a side-project.       ┃
┃ Please consider making a financial contribution according to your means if  ┃
┃ this is useful for you to ensure the long-term viability. Thank you :-)     ┃
┃                                                                             ┃
┃                   https://www.goatcounter.com/contribute                    ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛

`)
}

func flagStripe(stripe string, v *zvalidate.Validator) {
	if stripe == "" {
		v.Required("-stripe", stripe)
		return
	}

	for _, k := range zstring.Fields(stripe, ":") {
		switch {
		case strings.HasPrefix(k, "sk_"):
			zstripe.SecretKey = k
		case strings.HasPrefix(k, "pk_"):
			zstripe.PublicKey = k
		case strings.HasPrefix(k, "whsec_"):
			zstripe.SignSecret = k
		}
	}
	if zstripe.SecretKey == "" {
		v.Append("-stripe", "missing secret key (sk_)")
	}
	if zstripe.PublicKey == "" {
		v.Append("-stripe", "missing public key (pk_)")
	}
	if zstripe.SignSecret == "" {
		v.Append("-stripe", "missing signing secret (whsec_)")
	}
}

func flagDomain(domain string, v *zvalidate.Validator) {
	l := strings.Split(domain, ",")

	switch len(l) {
	default:
		v.Append("-domain", "too many domains")
	case 0:
		v.Append("-domain", "cannot be blank")
	case 1:
		v.Append("-domain", "must have static domain")
	case 2, 3:
		for i, d := range l {
			d = strings.TrimSpace(d)
			if p := strings.Index(d, ":"); p > -1 {
				v.Domain("-domain", d[:p])
			} else {
				v.Domain("-domain", d)
			}

			switch i {
			case 0:
				cfg.Domain = d
			case 1:
				cfg.DomainStatic = d
				cfg.DomainCount = d
				cfg.URLStatic = "//" + d
			case 2:
				cfg.DomainCount = d
			}
		}
	}
}
