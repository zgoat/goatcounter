// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package goatcounter

import (
	"context"
	"fmt"
	"sort"
	"time"

	"zgo.at/errors"
	"zgo.at/zdb"
)

type AdminStat struct {
	ID            int64     `db:"site_id"`
	Parent        *int64    `db:"parent"`
	Code          string    `db:"code"`
	Stripe        *string   `db:"stripe"`
	BillingAmount *string   `db:"billing_amount"`
	LinkDomain    string    `db:"link_domain"`
	Email         string    `db:"email"`
	CreatedAt     time.Time `db:"created_at"`
	Plan          string    `db:"plan"`
	LastMonth     int       `db:"last_month"`
	Total         int       `db:"total"`
}

type AdminStats []AdminStat

// List stats for all sites, for all time.
func (a *AdminStats) List(ctx context.Context) error {
	err := zdb.Select(ctx, a, fmt.Sprintf(`/* AdminStats.List */
		select
			sites.site_id,
			sites.parent,
			sites.code,
			sites.created_at,
			sites.billing_amount,
			(case
				when sites.stripe is null then 'free'
				when substr(sites.stripe, 0, 9) = 'cus_free' then 'free'
				else sites.plan
			end) as plan,
			stripe,
			sites.link_domain,
			(select email from users where site_id=sites.site_id or site_id=sites.parent) as email,
			coalesce((
				select sum(hit_counts.total) from hit_counts where site_id=sites.site_id
			), 0) as total,
			coalesce((
				select sum(hit_counts.total) from hit_counts
				where site_id=sites.site_id and hit_counts.hour >= %s
			), 0) as last_month
		from sites
		order by last_month desc`, interval(ctx, 30)))
	if err != nil {
		return errors.Wrap(err, "AdminStats.List")
	}

	// Add all the child plan counts to the parents.
	aa := *a
	for _, s := range aa {
		if s.Plan != PlanChild {
			continue
		}

		for i, s2 := range aa {
			if s2.ID == *s.Parent {
				aa[i].Total += s.Total
				aa[i].LastMonth += s.LastMonth
				break
			}
		}
	}
	sort.Slice(aa, func(i, j int) bool { return aa[i].LastMonth > aa[j].LastMonth })
	return nil
}

type AdminSiteStat struct {
	Site           Site      `db:"-"`
	Users          Users     `db:"-"`
	LastData       time.Time `db:"last_data"`
	CountTotal     int       `db:"count_total"`
	CountLastMonth int       `db:"count_last_month"`
	CountPrevMonth int       `db:"count_prev_month"`
}

// ByID gets stats for a single site.
func (a *AdminSiteStat) ByID(ctx context.Context, id int64) error {
	err := a.Site.ByID(ctx, id)
	if err != nil {
		return err
	}

	err = a.Users.List(ctx, id)
	if err != nil {
		return err
	}

	ival30 := interval(ctx, 30)
	ival60 := interval(ctx, 30)
	err = zdb.Get(ctx, a, fmt.Sprintf(`/* *AdminSiteStat.ByID */
		select
			coalesce((select hour from hit_counts where site_id=$1 order by hour desc limit 1), '1970-01-01') as last_data,
			coalesce((select sum(total) from hit_counts where site_id=$1), 0) as count_total,
			coalesce((select sum(total) from hit_counts where site_id=$1
				and hour >= %[1]s), 0) as count_last_month,
			coalesce((select sum(total) from hit_counts where site_id=$1
				and hour >= %[2]s
				and hour <= %[1]s
			), 0) as count_prev_month
		`, ival30, ival60), id)
	return errors.Wrap(err, "AdminSiteStats.ByID")
}

// ByCode gets stats for a single site.
func (a *AdminSiteStat) ByCode(ctx context.Context, code string) error {
	err := a.Site.ByHost(ctx, code+"."+Config(ctx).Domain)
	if err != nil {
		return err
	}
	return a.ByID(ctx, a.Site.ID)
}
