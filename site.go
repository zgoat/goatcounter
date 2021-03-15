// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package goatcounter

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"zgo.at/errors"
	"zgo.at/guru"
	"zgo.at/zdb"
	"zgo.at/zlog"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/znet"
	"zgo.at/zvalidate"
)

// Plan column values.
const (
	PlanPersonal     = "personal"
	PlanStarter      = "personalplus"
	PlanBusiness     = "business"
	PlanBusinessPlus = "businessplus"
	PlanChild        = "child"
)

var Plans = []string{PlanPersonal, PlanStarter, PlanBusiness, PlanBusinessPlus}

var reserved = []string{
	"www", "mail", "smtp", "imap", "static",
	"admin", "ns1", "ns2", "m", "mobile", "api",
	"dev", "test", "beta", "new", "staging", "debug", "pprof",
	"chat", "example", "yoursite", "test", "sql", "license",
}

var statTables = []string{"hit_stats", "system_stats", "browser_stats",
	"location_stats", "size_stats"}

type Site struct {
	ID     int64  `db:"site_id" json:"id,readonly"`
	Parent *int64 `db:"parent" json:"parent,readonly"`

	// Custom domain, e.g. "stats.example.com"
	Cname *string `db:"cname" json:"cname"`

	// When the CNAME was verified.
	CnameSetupAt *time.Time `db:"cname_setup_at" json:"cname_setup_at,readonly"`

	// Domain code (arp242, which makes arp242.goatcounter.com). Only used for
	// goatcounter.com and not when self-hosting.
	Code string `db:"code" json:"code"`

	// Site domain for linking (www.arp242.net).
	LinkDomain    string       `db:"link_domain" json:"link_domain"`
	Plan          string       `db:"plan" json:"plan"`
	PlanPending   *string      `db:"plan_pending" json:"plan_pending"`
	Stripe        *string      `db:"stripe" json:"-"`
	PlanCancelAt  *time.Time   `db:"plan_cancel_at" json:"plan_cancel_at"`
	BillingAmount *string      `db:"billing_amount" json:"-"`
	Settings      SiteSettings `db:"settings" json:"setttings"`

	// Whether this site has received any data; will be true after the first
	// pageview.
	ReceivedData bool `db:"received_data" json:"received_data"`

	State      string     `db:"state" json:"state"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt  *time.Time `db:"updated_at" json:"updated_at"`
	FirstHitAt time.Time  `db:"first_hit_at" json:"first_hit_at"`
}

// ClearCache clears the  cache for this site.
func (s Site) ClearCache(ctx context.Context, full bool) {
	cacheSites(ctx).Delete(strconv.FormatInt(s.ID, 10))

	// TODO: be more selective about this.
	if full {
		cachePaths(ctx).Flush()
		cacheChangedTitles(ctx).Flush()
	}
}

// Defaults sets fields to default values, unless they're already set.
func (s *Site) Defaults(ctx context.Context) {
	if s.State == "" {
		s.State = StateActive
	}

	s.Code = strings.ToLower(s.Code)

	if s.CreatedAt.IsZero() {
		s.CreatedAt = Now()
	} else {
		t := Now()
		s.UpdatedAt = &t
	}
	if s.FirstHitAt.IsZero() {
		s.FirstHitAt = Now()
	}

	s.Settings.Defaults()
}

var noUnderscore = time.Date(2020, 03, 20, 0, 0, 0, 0, time.UTC)

// Validate the object.
func (s *Site) Validate(ctx context.Context) error {
	v := zvalidate.New()

	v.Required("code", s.Code)
	v.Required("state", s.State)
	v.Required("plan", s.Plan)
	v.Include("state", s.State, States)
	if s.Parent == nil {
		v.Include("plan", s.Plan, Plans)
	} else {
		v.Include("plan", s.Plan, []string{PlanChild})
	}

	if s.PlanPending != nil {
		v.Include("plan_pending", *s.PlanPending, Plans)
		if s.Parent != nil {
			v.Append("plan_pending", "can't be set if there's a parent")
		}
	}

	// Must always include all widgets we know about.
	for _, w := range defaultWidgets() {
		if s.Settings.Widgets.Get(w["name"].(string)) == nil {
			v.Append("widgets", fmt.Sprintf("widget %q is missing", w["name"].(string)))
		}
	}
	v.Range("widgets.pages.s.limit_pages", int64(s.Settings.LimitPages()), 1, 100)
	v.Range("widgets.pages.s.limit_refs", int64(s.Settings.LimitRefs()), 1, 25)

	if _, i := s.Settings.Views.Get("default"); i == -1 || len(s.Settings.Views) != 1 {
		v.Append("views", "view not set")
	}

	if s.Settings.DataRetention > 0 {
		v.Range("settings.data_retention", int64(s.Settings.DataRetention), 14, 0)
	}

	if len(s.Settings.IgnoreIPs) > 0 {
		for _, ip := range s.Settings.IgnoreIPs {
			v.IP("settings.ignore_ips", ip)
		}
	}

	v.Domain("link_domain", s.LinkDomain)
	v.Len("code", s.Code, 2, 50)
	v.Exclude("code", s.Code, reserved)
	// TODO: compat with older requirements, otherwise various update functions
	// will error out.
	if !s.CreatedAt.IsZero() && s.CreatedAt.Before(noUnderscore) {
		for _, c := range s.Code {
			if !(c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
				v.Append("code", fmt.Sprintf("%q not allowed; characters are limited to '_', '-', a to z, and numbers", c))
				break
			}
		}
		if len(s.Code) > 0 && (s.Code[0] == '_' || s.Code[0] == '-') { // Special domains, like _acme-challenge.
			v.Append("code", "cannot start with underscore or dash (_, -)")
		}
	} else {
		labels := v.Hostname("code", s.Code)
		if len(labels) > 1 {
			v.Append("code", "cannot contain '.'")
		}
	}

	if s.Stripe != nil && !strings.HasPrefix(*s.Stripe, "cus_") {
		v.Append("stripe", "not a valid Stripe customer ID")
	}

	if s.Cname != nil {
		v.Len("cname", *s.Cname, 4, 255)
		v.Domain("cname", *s.Cname)
		if Config(ctx).GoatcounterCom && strings.HasSuffix(*s.Cname, Config(ctx).Domain) {
			v.Append("cname", "cannot end with %q", Config(ctx).Domain)
		}

	}

	if !v.HasErrors() {
		exists, err := s.Exists(ctx)
		if err != nil {
			return err
		}
		if exists > 0 {
			field := "code"
			if s.Cname != nil {
				field = "cname"
			}
			v.Append(field, "already exists")
		}
	}

	return v.ErrorOrNil()
}

// Insert a new row.
func (s *Site) Insert(ctx context.Context) error {
	if s.ID > 0 {
		return errors.New("ID > 0")
	}
	s.Defaults(ctx)
	err := s.Validate(ctx)
	if err != nil {
		return err
	}

	s.ID, err = zdb.InsertID(ctx, "site_id",
		`insert into sites (parent, code, cname, link_domain, settings, plan, created_at, first_hit_at) values (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Parent, s.Code, s.Cname, s.LinkDomain, s.Settings, s.Plan, s.CreatedAt, s.CreatedAt)
	if err != nil && zdb.ErrUnique(err) {
		return guru.New(400, "this site already exists: code or domain must be unique")
	}
	return errors.Wrap(err, "Site.Insert")
}

// Update existing site. Sets settings, cname, link_domain.
func (s *Site) Update(ctx context.Context) error {
	if s.ID == 0 {
		return errors.New("ID == 0")
	}
	s.Defaults(ctx)
	err := s.Validate(ctx)
	if err != nil {
		return err
	}

	err = zdb.Exec(ctx,
		`update sites set settings=?, cname=?, link_domain=?, updated_at=? where site_id=?`,
		s.Settings, s.Cname, s.LinkDomain, s.UpdatedAt, s.ID)
	if err != nil {
		return errors.Wrap(err, "Site.Update")
	}

	s.ClearCache(ctx, false)
	return nil
}

// UpdateStripe sets the billing info.
func (s *Site) UpdateStripe(ctx context.Context) error {
	if s.ID == 0 {
		return errors.New("ID == 0")
	}
	s.Defaults(ctx)
	err := s.Validate(ctx)
	if err != nil {
		return err
	}

	err = zdb.Exec(ctx,
		`update sites set stripe=?, plan=?, plan_pending=?, billing_amount=?, plan_cancel_at=?, updated_at=? where site_id=?`,
		s.Stripe, s.Plan, s.PlanPending, s.BillingAmount, s.PlanCancelAt, s.UpdatedAt, s.ID)
	if err != nil {
		return errors.Wrap(err, "Site.UpdateStripe")
	}

	s.ClearCache(ctx, false)
	return nil
}

func (s *Site) UpdateParent(ctx context.Context, newParent *int64) error {
	if s.ID == 0 {
		return errors.New("ID == 0")
	}

	s.Parent = newParent
	if newParent != nil {
		s.Plan = PlanChild
	}

	s.Defaults(ctx)
	err := s.Validate(ctx)
	if err != nil {
		return err
	}

	err = zdb.Exec(ctx,
		`update sites set parent=?, plan=?, updated_at=? where site_id=?`,
		s.Parent, s.Plan, s.UpdatedAt, s.ID)
	if err != nil {
		return errors.Wrap(err, "Site.UpdateParent")
	}

	s.ClearCache(ctx, false)
	return nil
}

// UpdateCode changes the site's domain code (e.g. "test" in
// "test.goatcounter.com").
func (s *Site) UpdateCode(ctx context.Context, code string) error {
	if s.ID == 0 {
		return errors.New("ID == 0")
	}

	s.Code = code

	s.Defaults(ctx)
	err := s.Validate(ctx)
	if err != nil {
		return err
	}

	err = zdb.Exec(ctx,
		`update sites set code=$1, updated_at=$2 where site_id=$3`,
		s.Code, s.UpdatedAt, s.ID)
	if err != nil {
		return errors.Wrap(err, "Site.UpdateCode")
	}

	cacheSites(ctx).Delete(strconv.FormatInt(s.ID, 10))
	cacheSitesHost(ctx).Flush()
	return nil
}

func (s *Site) UpdateReceivedData(ctx context.Context) error {
	err := zdb.Exec(ctx, `update sites set received_data=1 where site_id=$1`, s.ID)

	s.ClearCache(ctx, false)
	return errors.Wrap(err, "Site.UpdateReceivedData")
}

func (s *Site) UpdateFirstHitAt(ctx context.Context, f time.Time) error {
	f = f.UTC().Add(-12 * time.Hour)
	s.FirstHitAt = f
	err := zdb.Exec(ctx,
		`update sites set first_hit_at=$1 where site_id=$2`,
		s.FirstHitAt, s.ID)

	s.ClearCache(ctx, false)
	return errors.Wrap(err, "Site.UpdateFirstHitAt")
}

// UpdateCnameSetupAt confirms the custom domain was setup correct.
func (s *Site) UpdateCnameSetupAt(ctx context.Context) error {
	if s.ID == 0 {
		return errors.New("ID == 0")
	}

	n := Now()
	s.CnameSetupAt = &n

	err := zdb.Exec(ctx,
		`update sites set cname_setup_at=$1 where site_id=$2`,
		s.CnameSetupAt, s.ID)
	if err != nil {
		return errors.Wrap(err, "Site.UpdateCnameSetupAt")
	}

	s.ClearCache(ctx, false)
	return nil
}

// Delete a site and all child sites.
func (s *Site) Delete(ctx context.Context, deleteChildren bool) error {
	if s.ID == 0 {
		return errors.New("ID == 0")
	}

	if !deleteChildren {
		var has int
		err := zdb.Get(ctx, &has, `select 1 from sites where parent = ?`, s.ID)
		if err != nil {
			return errors.Wrap(err, "Site.Delete")
		}
		if has == 1 {
			return fmt.Errorf("Site.Delete: site %d has linked sites", s.ID)
		}
	}

	t := Now()
	err := zdb.Exec(ctx,
		`update sites set state=$1, updated_at=$2 where site_id=$3 or parent=$3`,
		StateDeleted, t, s.ID)
	if err != nil {
		return errors.Wrap(err, "Site.Delete")
	}

	s.ClearCache(ctx, true)

	s.ID = 0
	s.UpdatedAt = &t
	s.State = StateDeleted
	return nil
}

func (s Site) Undelete(ctx context.Context, id int64) error {
	s.State = StateActive
	s.ID = id
	err := zdb.Exec(ctx, `update sites set state = ? where site_id = ?`, StateActive, id)
	if err != nil {
		return fmt.Errorf("Site.Undelete %d: %w", id, err)
	}

	s.ClearCache(ctx, false)
	return errors.Wrap(s.ByID(ctx, id), "Site.Undelete")
}

// Exists checks if this site already exists, based on either the Cname or Code
// field.
func (s Site) Exists(ctx context.Context) (int64, error) {
	var (
		id     int64
		query  = `select site_id from sites where lower(code) = lower($1) and site_id != $2 limit 1`
		params = zdb.L{s.Code, s.ID}
	)
	if s.Cname != nil {
		query = `select site_id from sites where lower(cname) = lower($1) and site_id != $2 limit 1`
		params = zdb.L{s.Cname, s.ID}
	}

	err := zdb.Get(ctx, &id, query, params...)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("Site.Exists: %w", err)
	}
	return id, nil
}

// ByID gets a site by ID.
func (s *Site) ByID(ctx context.Context, id int64) error {
	err := s.ByIDState(ctx, id, StateActive)
	if err != nil {
		return fmt.Errorf("Site.ByID: %w", errors.Unwrap(err))
	}
	return nil
}

// ByIDState gets a site by ID and state. This may return deleted sites.
func (s *Site) ByIDState(ctx context.Context, id int64, state string) error {
	k := strconv.FormatInt(id, 10)
	ss, ok := cacheSites(ctx).Get(k)
	if ok {
		*s = *ss.(*Site)
		return nil
	}

	err := zdb.Get(ctx, s,
		`/* Site.ByID */ select * from sites where site_id=$1 and state=$2`,
		id, state)
	if err != nil {
		return errors.Wrapf(err, "Site.ByIDState %d", id)
	}
	cacheSites(ctx).SetDefault(k, s)
	return nil
}

// ByStripe gets a site by the Stripe customer ID.
func (s *Site) ByStripe(ctx context.Context, stripe string) error {
	err := zdb.Get(ctx, s, `select * from sites where stripe=$1`, stripe)
	return errors.Wrapf(err, "Site.ByStripe %s", stripe)
}

// ByCode gets a site by code.
func (s *Site) ByCode(ctx context.Context, code string) error {
	return errors.Wrapf(zdb.Get(ctx, s,
		`/* Site.ByCode */ select * from sites where code=$1 and state=$2`,
		code, StateActive), "Site.ByCode %s", code)
}

// ByHost gets a site by host name.
func (s *Site) ByHost(ctx context.Context, host string) error {
	ss, ok := cacheSitesHost(ctx).Get(host)
	if ok {
		*s = *ss.(*Site)
		return nil
	}

	// Custom domain or serve.
	if Config(ctx).Serve || !strings.HasSuffix(host, Config(ctx).Domain) {
		err := zdb.Get(ctx, s,
			`/* Site.ByHost */ select * from sites where lower(cname)=lower($1) and state=$2`,
			znet.RemovePort(host), StateActive)
		if err != nil {
			return errors.Wrap(err, "site.ByHost: from custom domain")
		}
		cacheSitesHost(ctx).Set(strconv.FormatInt(s.ID, 10), host, s)
		return nil
	}

	// Get from code (e.g. "arp242" in "arp242.goatcounter.com").
	p := strings.Index(host, ".")
	if p == -1 {
		return errors.Errorf("Site.ByHost: no subdomain in host %q", host)
	}

	err := zdb.Get(ctx, s,
		`/* Site.ByHost */ select * from sites where lower(code)=lower($1) and state=$2`,
		host[:p], StateActive)
	if err != nil {
		return errors.Wrap(err, "site.ByHost: from code")
	}
	cacheSitesHost(ctx).Set(strconv.FormatInt(s.ID, 10), host, s)
	return nil
}

// Find a site: by ID if ident is a number, or by host if it's not.
func (s *Site) Find(ctx context.Context, ident string) error {
	id, err := strconv.ParseInt(ident, 10, 64)
	if err == nil {
		err = s.ByID(ctx, id)
	} else {
		err = s.ByHost(ctx, ident)
	}
	return errors.Wrap(err, "Site.Find")
}

// ListSubs lists all subsites, including the current site and parent.
func (s *Site) ListSubs(ctx context.Context) ([]string, error) {
	col := "code"
	if Config(ctx).Serve {
		col = "cname"
	}
	var codes []string
	err := zdb.Select(ctx, &codes, `/* Site.ListSubs */
		select `+col+` from sites
		where state=$1 and (parent=$2 or site_id=$2) or (
			parent  = (select parent from sites where site_id=$2) or
			site_id = (select parent from sites where site_id=$2)
		) and state=$1
		order by code
		`, StateActive, s.ID)
	return codes, errors.Wrap(err, "Site.ListSubs")
}

// Domain gets the global default domain, or this site's configured custom
// domain.
func (s Site) Domain(ctx context.Context) string {
	if s.Cname != nil && s.CnameSetupAt != nil {
		return *s.Cname
	}
	return Config(ctx).Domain
}

// Display format: just the domain (cname or code+domain).
func (s Site) Display(ctx context.Context) string {
	if s.Cname != nil && s.CnameSetupAt != nil {
		return *s.Cname
	}
	return fmt.Sprintf("%s.%s", s.Code, znet.RemovePort(Config(ctx).Domain))
}

// URL to this site.
func (s Site) URL(ctx context.Context) string {
	if s.Cname != nil && s.CnameSetupAt != nil {
		return fmt.Sprintf("http%s://%s%s",
			map[bool]string{true: "", false: "s"}[Config(ctx).Dev],
			*s.Cname, Config(ctx).Port)
	}

	return fmt.Sprintf("http%s://%s.%s%s",
		map[bool]string{true: "", false: "s"}[Config(ctx).Dev],
		s.Code, Config(ctx).Domain, Config(ctx).Port)
}

// PlanCustomDomain reports if this site's plan allows custom domains.
func (s Site) PlanCustomDomain(ctx context.Context) bool {
	if s.Parent != nil {
		var ps Site
		err := ps.ByID(ctx, *s.Parent)
		if err != nil {
			zlog.Error(err)
			return false
		}
		return ps.PlanCustomDomain(ctx)
	}
	return s.Plan == PlanStarter || s.Plan == PlanBusiness || s.Plan == PlanBusinessPlus
}

// IDOrParent gets this site's ID or the parent ID if that's set.
func (s Site) IDOrParent() int64 {
	if s.Parent != nil {
		return *s.Parent
	}
	return s.ID
}

// GetMain gets the "main" site for this account; either the current site or the
// parent one.
func (s *Site) GetMain(ctx context.Context) error {
	if s.ID == 0 {
		return errors.New("s.ID == 0")
	}
	if s.Parent != nil {
		return s.ByID(ctx, *s.Parent)
	}
	return nil
}

//lint:ignore U1001 used in template (via ShowPayBanner)
var trialPeriod = time.Hour * 24 * 14

// ShowPayBanner determines if we should show a "please pay" banner for the
// customer.
//
//lint:ignore U1001 used in template.
func (s Site) ShowPayBanner(ctx context.Context) bool {
	if s.Parent != nil {
		var ps Site
		err := ps.ByID(ctx, *s.Parent)
		if err != nil {
			zlog.Error(err)
			return false
		}
		return ps.ShowPayBanner(ctx)
	}

	if s.Stripe != nil {
		return false
	}
	return -Now().Sub(s.CreatedAt.Add(trialPeriod)) < 0
}

func (s Site) FreePlan() bool {
	return s.Stripe != nil && strings.HasPrefix(*s.Stripe, "cus_free_")
}

// PayExternal gets the external payment name, or an empty string if there is
// none.
func (s Site) PayExternal() string {
	if s.Stripe == nil {
		return ""
	}

	if strings.HasPrefix(*s.Stripe, "cus_github_") {
		return "GitHub Sponsors"
	}
	if strings.HasPrefix(*s.Stripe, "cus_patreon_") {
		return "Patreon"
	}
	return ""
}

// DeleteAll deletes all pageviews for this site, keeping the site itself and
// user intact.
func (s Site) DeleteAll(ctx context.Context) error {
	return zdb.TX(ctx, func(ctx context.Context) error {
		for _, t := range append(statTables, "hit_counts", "ref_counts", "hits", "paths") {
			err := zdb.Exec(ctx, `delete from `+t+` where site_id=:id`, zdb.P{"id": s.ID})
			if err != nil {
				return errors.Wrap(err, "Site.DeleteAll: delete "+t)
			}
		}

		s.ClearCache(ctx, true)
		return nil
	})
}

func (s Site) DeleteOlderThan(ctx context.Context, days int) error {
	if days < 14 {
		return errors.Errorf("days must be at least 14: %d", days)
	}

	return zdb.TX(ctx, func(ctx context.Context) error {
		ival := interval(ctx, days)

		var pathIDs []int64
		err := zdb.Select(ctx, &pathIDs, `/* Site.DeleteOlderThan */
			select path_id from hit_counts where site_id=$1 and hour < `+ival+` group by path_id`, s.ID)
		if err != nil {
			return errors.Wrap(err, "Site.DeleteOlderThan: get paths")
		}

		for _, t := range statTables {
			err := zdb.Exec(ctx, `delete from `+t+` where site_id=$1 and day < `+ival, s.ID)
			if err != nil {
				return errors.Wrap(err, "Site.DeleteOlderThan: delete "+t)
			}
		}

		err = zdb.Exec(ctx, `delete from hit_counts where site_id=$1 and hour < `+ival, s.ID)
		if err != nil {
			return errors.Wrap(err, "Site.DeleteOlderThan: delete hit_counts")
		}
		err = zdb.Exec(ctx, `delete from ref_counts where site_id=$1 and hour < `+ival, s.ID)
		if err != nil {
			return errors.Wrap(err, "Site.DeleteOlderThan: delete ref_counts")
		}

		err = zdb.Exec(ctx, `delete from hits where site_id=$1 and created_at < `+ival, s.ID)
		if err != nil {
			return errors.Wrap(err, "Site.DeleteOlderThan: delete hits")
		}

		if len(pathIDs) > 0 {
			query, args, err := sqlx.In(`/* Site.DeleteOlderThan */
				select path_id from hit_counts where site_id=? and path_id in (?)`,
				s.ID, pathIDs)
			if err != nil {
				return errors.Wrap(err, "Site.DeleteOlderThan")
			}
			var remainPath []int64
			err = zdb.Select(ctx, &remainPath, query, args...)
			if err != nil {
				return errors.Wrap(err, "Site.DeleteOlderThan")
			}

			diff := zint.Difference(pathIDs, remainPath)
			if len(diff) > 0 {
				query, args, err := sqlx.In(`delete from paths where site_id=? and path_id in (?)`, s.ID, diff)
				if err != nil {
					return errors.Wrap(err, "Site.DeleteOlderThan")
				}
				err = zdb.Exec(ctx, query, args...)
				if err != nil {
					return errors.Wrap(err, "Site.DeleteOlderThan")
				}
			}
		}

		return nil
	})
}

// Admin reports if this site is an admin.
func (s Site) Admin() bool {
	return s.ID == 1
}

// Sites is a list of sites.
type Sites []Site

// UnscopedList lists all sites, not scoped to the current user.
func (s *Sites) UnscopedList(ctx context.Context) error {
	return errors.Wrap(zdb.Select(ctx, s,
		`/* Sites.List */ select * from sites where state=$1`,
		StateActive), "Sites.List")
}

// UnscopedListCnames all sites that have CNAME set, not scoped to the current
// user.
func (s *Sites) UnscopedListCnames(ctx context.Context) error {
	return errors.Wrap(zdb.Select(ctx, s, `/* Sites.ListCnames */
		select * from sites where state=$1 and cname is not null`,
		StateActive), "Sites.List")
}

// ListSubs lists all subsites for the current site.
func (s *Sites) ListSubs(ctx context.Context) error {
	return errors.Wrap(zdb.Select(ctx, s, `/* Sites.ListSubs */
		select * from sites where parent=$1 and state=$2 order by code`,
		MustGetSite(ctx).ID, StateActive), "Sites.ListSubs")
}

// ForThisAccount gets all sites associated with this account.
func (s *Sites) ForThisAccount(ctx context.Context, excludeCurrent bool) error {
	site := MustGetSite(ctx)
	err := zdb.Select(ctx, s, `/* Sites.ForThisAccount */
		select * from sites
		where state=$1 and (parent=$2 or site_id=$2) or (
			parent  = (select parent from sites where site_id=$2) or
			site_id = (select parent from sites where site_id=$2)
		) and state=$1
		order by code
		`, StateActive, site.ID)
	if err != nil {
		return errors.Wrap(err, "Sites.ForThisAccount")
	}

	if excludeCurrent {
		ss := *s
		for i := range ss {
			if ss[i].ID == site.ID {
				*s = append(ss[:i], ss[i+1:]...)
				break
			}
		}
	}

	return nil
}

// ContainsCNAME reports if there is a site with this CNAME set.
func (s *Sites) ContainsCNAME(ctx context.Context, cname string) (bool, error) {
	var ok bool
	err := zdb.Get(ctx, &ok, `/* Sites.ContainsCNAME */
		select 1 from sites where lower(cname)=lower($1) limit 1`, cname)
	return ok, errors.Wrapf(err, "Sites.ContainsCNAME for %q", cname)
}

// OldSoftDeleted finds all sites which have been soft-deleted more than a week
// ago.
func (s *Sites) OldSoftDeleted(ctx context.Context) error {
	return errors.Wrap(zdb.Select(ctx, s, fmt.Sprintf(`/* Sites.OldSoftDeleted */
		select * from sites where state=$1 and updated_at < %s`, interval(ctx, 7)),
		StateDeleted), "Sites.OldSoftDeleted")
}

// ExpiredPlans finds all sites which have a plan that's expired.
func (s *Sites) ExpiredPlans(ctx context.Context) error {
	err := zdb.Select(ctx, s, `/* Sites.ExpiredPlans */
		select * from sites where ? > plan_cancel_at`, Now())
	return errors.Wrap(err, "Sites.ExpiredPlans")
}
