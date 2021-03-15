// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"zgo.at/goatcounter"
	"zgo.at/goatcounter/bgrun"
	"zgo.at/goatcounter/gctest"
	"zgo.at/zdb"
)

func TestSettingsTpl(t *testing.T) {
	tests := []handlerTest{
		{
			setup: func(ctx context.Context, t *testing.T) {
				now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)
				gctest.StoreHits(ctx, t, false, []goatcounter.Hit{
					{Site: 1, Path: "/asd", Title: "AAA", CreatedAt: now},
					{Site: 1, Path: "/asd", Title: "AAA", CreatedAt: now},
					{Site: 1, Path: "/zxc", Title: "BBB", CreatedAt: now},
				}...)
			},
			router:   newBackend,
			path:     "/settings/purge/confirm?path=/asd",
			auth:     true,
			wantCode: 200,
			wantBody: "<tr><td>2</td><td>/asd</td><td>AAA</td></tr>",
		},

		{
			setup: func(ctx context.Context, t *testing.T) {
				one := int64(1)
				ss := goatcounter.Site{
					Code:   "subsite",
					Parent: &one,
					Plan:   goatcounter.PlanChild,
				}
				err := ss.Insert(ctx)
				if err != nil {
					panic(err)
				}
			},
			router:   newBackend,
			path:     "/settings/sites/remove/2",
			auth:     true,
			wantCode: 200,
			wantBody: "Are you sure you want to remove the site",
		},
	}

	for _, tt := range tests {
		runTest(t, tt, nil)
	}
}

func TestSettingsPurge(t *testing.T) {
	tests := []handlerTest{
		{
			setup: func(ctx context.Context, t *testing.T) {
				now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)
				gctest.StoreHits(ctx, t, false, []goatcounter.Hit{
					{Site: 1, Path: "/asd", CreatedAt: now},
					{Site: 1, Path: "/asd", CreatedAt: now},
					{Site: 1, Path: "/zxc", CreatedAt: now},
				}...)
			},
			router:       newBackend,
			path:         "/settings/purge",
			body:         map[string]string{"path": "/asd", "paths": "1,"},
			method:       "POST",
			auth:         true,
			wantFormCode: 303,
		},
	}

	for _, tt := range tests {
		runTest(t, tt, func(t *testing.T, rr *httptest.ResponseRecorder, r *http.Request) {
			bgrun.Wait()

			var hits goatcounter.Hits
			err := hits.TestList(r.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != 1 {
				t.Errorf("%d hits in DB; expected 1:\n%v", len(hits), hits)
			}
		})
	}
}

func TestSettingsSitesAdd(t *testing.T) {
	tests := []handlerTest{
		{
			name:         "new site",
			setup:        func(ctx context.Context, t *testing.T) {},
			router:       newBackend,
			path:         "/settings/sites/add",
			body:         map[string]string{"cname": "add.example.com"},
			method:       "POST",
			auth:         true,
			wantFormCode: 303,
			want: `
				site_id  code   cname            plan      parent  state
				1        gctes  NULL             personal  NULL    a
				2        serve  add.example.com  child     1       a`,
		},
		{
			name: "already exists for this account",
			setup: func(ctx context.Context, t *testing.T) {
				one := int64(1)
				cn := "add.example.com"
				s := goatcounter.Site{
					Parent: &one,
					Cname:  &cn,
					Code:   "add",
					Plan:   goatcounter.PlanChild,
				}
				err := s.Insert(ctx)
				if err != nil {
					t.Fatal(err)
				}
			},
			router:       newBackend,
			path:         "/settings/sites/add",
			body:         map[string]string{"cname": "add.example.com"},
			method:       "POST",
			auth:         true,
			wantFormCode: 400,
			wantFormBody: "already exists",
			want: `
				site_id  code   cname            plan      parent  state
				1        gctes  NULL             personal  NULL    a
				2        add    add.example.com  child     1       a`,
		},
		{
			name: "already exists on other account",
			setup: func(ctx context.Context, t *testing.T) {
				cn := "add.example.com"
				s := goatcounter.Site{
					Cname: &cn,
					Code:  "add",
					Plan:  goatcounter.PlanPersonal,
				}
				err := s.Insert(ctx)
				if err != nil {
					t.Fatal(err)
				}
			},
			router:       newBackend,
			path:         "/settings/sites/add",
			body:         map[string]string{"cname": "add.example.com"},
			method:       "POST",
			auth:         true,
			wantFormCode: 400,
			wantFormBody: "already exists",
			want: `
				site_id  code   cname            plan      parent  state
				1        gctes  NULL             personal  NULL    a
				2        add    add.example.com  personal  NULL    a`,
		},
		{
			name: "undelete",
			setup: func(ctx context.Context, t *testing.T) {
				one := int64(1)
				cn := "add.example.com"
				s := goatcounter.Site{
					Parent: &one,
					Cname:  &cn,
					Code:   "add",
					Plan:   goatcounter.PlanChild,
				}
				err := s.Insert(ctx)
				if err != nil {
					t.Fatal(err)
				}
				err = s.Delete(ctx, false)
				if err != nil {
					t.Fatal(err)
				}
			},
			router:       newBackend,
			path:         "/settings/sites/add",
			body:         map[string]string{"cname": "add.example.com"},
			method:       "POST",
			auth:         true,
			wantFormCode: 303,
			want: `
				site_id  code   cname            plan      parent  state
				1        gctes  NULL             personal  NULL    a
				2        add    add.example.com  child     1       a`,
		},
		{
			name: "undelete other account",
			setup: func(ctx context.Context, t *testing.T) {
				cn := "add.example.com"
				s := goatcounter.Site{
					Cname: &cn,
					Code:  "add",
					Plan:  goatcounter.PlanPersonal,
				}
				err := s.Insert(ctx)
				if err != nil {
					t.Fatal(err)
				}
				err = s.Delete(ctx, false)
				if err != nil {
					t.Fatal(err)
				}
			},
			router:       newBackend,
			path:         "/settings/sites/add",
			body:         map[string]string{"cname": "add.example.com"},
			method:       "POST",
			auth:         true,
			wantFormCode: 400,
			wantFormBody: "already exists",
			want: `
				site_id  code   cname            plan      parent  state
				1        gctes  NULL             personal  NULL    a
				2        add    add.example.com  personal  NULL    d`,
		},
	}

	for _, tt := range tests {
		runTest(t, tt, func(t *testing.T, rr *httptest.ResponseRecorder, r *http.Request) {
			got := zdb.DumpString(r.Context(), `select site_id, substr(code, 0, 6) as code, cname, plan, parent, state from sites`)
			if d := zdb.Diff(got, tt.want); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestSettingsSitesRemove(t *testing.T) {
	tests := []handlerTest{
		{
			name: "remove",
			setup: func(ctx context.Context, t *testing.T) {
				one := int64(1)
				cn := "add.example.com"
				s := goatcounter.Site{
					Parent: &one,
					Cname:  &cn,
					Code:   "add",
					Plan:   goatcounter.PlanChild,
				}
				err := s.Insert(ctx)
				if err != nil {
					t.Fatal(err)
				}
			},
			router:       newBackend,
			path:         "/settings/sites/remove/2",
			body:         map[string]string{"cname": "add.example.com"},
			method:       "POST",
			auth:         true,
			wantFormCode: 303,
			want: `
				site_id  code   cname            plan      parent  state
				1        gctes  NULL             personal  NULL    a
				2        add    add.example.com  child     1       d`,
		},
		{
			name:         "remove self",
			setup:        func(ctx context.Context, t *testing.T) {},
			router:       newBackend,
			path:         "/settings/sites/remove/1",
			body:         map[string]string{"cname": "add.example.com"},
			method:       "POST",
			auth:         true,
			wantFormCode: 303,
			want: `
				site_id  code   cname  plan      parent  state
				1        gctes  NULL   personal  NULL    d`,
		},
		{
			name: "remove other account",
			setup: func(ctx context.Context, t *testing.T) {
				cn := "add.example.com"
				s := goatcounter.Site{
					Cname: &cn,
					Code:  "add",
					Plan:  goatcounter.PlanPersonal,
				}
				err := s.Insert(ctx)
				if err != nil {
					t.Fatal(err)
				}
			},
			router:       newBackend,
			path:         "/settings/sites/remove/2",
			body:         map[string]string{"cname": "add.example.com"},
			method:       "POST",
			auth:         true,
			wantFormCode: 404,
			want: `
				site_id  code   cname            plan      parent  state
				1        gctes  NULL             personal  NULL    a
				2        add    add.example.com  personal  NULL    a`,
		},
	}

	for _, tt := range tests {
		runTest(t, tt, func(t *testing.T, rr *httptest.ResponseRecorder, r *http.Request) {
			got := zdb.DumpString(r.Context(), `select site_id, substr(code, 0, 6) as code, cname, plan, parent, state from sites`)
			if d := zdb.Diff(got, tt.want); d != "" {
				t.Error(d)
			}
		})
	}
}
