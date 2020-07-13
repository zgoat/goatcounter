// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"zgo.at/blackmail"
	"zgo.at/errors"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/bgrun"
	"zgo.at/goatcounter/cfg"
	"zgo.at/guru"
	"zgo.at/json"
	"zgo.at/zdb"
	"zgo.at/zhttp"
	"zgo.at/zlog"
	"zgo.at/zstd/zjson"
	"zgo.at/zstripe"
	"zgo.at/zvalidate"
)

type billing struct{}

func (h billing) mount(pub, auth chi.Router) {
	pub = pub.With(zhttp.Log(true, ""))
	auth = auth.With(zhttp.Log(true, ""))

	auth.Get("/billing", zhttp.Wrap(h.index))

	pauth := auth.With(noSubSites)
	pauth.Post("/billing/start", zhttp.Wrap(h.start))
	pauth.Get("/billing/cancel", zhttp.Wrap(h.confirmCancel))
	pauth.Post("/billing/cancel", zhttp.Wrap(h.cancel))

	// These are not specific to any domain.
	pub.Post("/stripe-webhook", zhttp.Wrap(h.stripeWebhook))
}

func (h billing) index(w http.ResponseWriter, r *http.Request) error {
	site := goatcounter.MustGetSite(r.Context())

	switch r.URL.Query().Get("return") {
	case "cancel":
		zhttp.FlashError(w, "Payment cancelled.")

	case "success":
		// Verify that the webhook was processed correct.
		if site.Stripe == nil || site.UpdatedAt.Before(goatcounter.Now().Add(-1*time.Minute)) {
			zhttp.Flash(w, "The payment processor reported success, but we're still processing the payment")
			stripe := ""
			if site.Stripe != nil {
				stripe = *site.Stripe
			}
			zlog.Fields(zlog.F{
				"siteID":   site.ID,
				"stripeID": stripe,
			}).Errorf("stripe not processed")
		} else {
			bgrun.Run(func() {
				blackmail.Send("New GoatCounter subscription "+site.Plan,
					blackmail.From("GoatCounter Billing", "billing@goatcounter.com"),
					blackmail.To("billing@goatcounter.com"),
					blackmail.Bodyf(`New subscription: %s (%d) %s`, site.Code, site.ID, *site.Stripe))
			})

			zhttp.Flash(w, "Payment processed successfully!")
		}
	}

	external := site.PayExternal()
	var payment, next string
	if external != "" {
		payment = external
	}
	if site.Stripe != nil && !site.FreePlan() && external == "" {
		var customer struct {
			Subscriptions struct {
				Data []struct {
					CancelAtPeriodEnd bool            `json:"cancel_at_period_end"`
					CurrentPeriodEnd  zjson.Timestamp `json:"current_period_end"`
					Plan              struct {
						Quantity int `json:"quantity"`
					} `json:"plan"`
				} `json:"data"`
			} `json:"subscriptions"`
		}
		_, err := zstripe.Request(&customer, "GET",
			fmt.Sprintf("/v1/customers/%s", *site.Stripe), "")
		if err != nil {
			return err
		}

		if len(customer.Subscriptions.Data) > 0 {
			var methods struct {
				Data []struct {
					Card struct {
						Brand string `json:"brand"`
						Last4 string `json:"last4"`
					} `json:"card"`
				} `json:"data"`
			}
			_, err = zstripe.Request(&methods, "GET", "/v1/payment_methods", zstripe.Body{
				"customer": *site.Stripe,
				"type":     "card",
			}.Encode())
			if err != nil {
				return err
			}
			if len(methods.Data) > 0 {
				payment = fmt.Sprintf("a %s card ending with %s",
					methods.Data[0].Card.Brand, methods.Data[0].Card.Last4)
			}

			var invoice struct {
				AmountDue int             `json:"amount_due"`
				Created   zjson.Timestamp `json:"created"`
			}
			_, err = zstripe.Request(&invoice, "GET", "/v1/invoices/upcoming", zstripe.Body{
				"customer": *site.Stripe,
			}.Encode())
			if err != nil {
				return err
			}
			next = fmt.Sprintf("Next invoice will be for €%d on %s.",
				invoice.AmountDue/100, invoice.Created.Format("Jan 2, 2006"))
		}
	}

	return zhttp.Template(w, "billing.gohtml", struct {
		Globals
		StripePublicKey string
		Payment         string
		Next            string
		Subscribed      bool
		FreePlan        bool
		External        string
	}{newGlobals(w, r), zstripe.PublicKey, payment, next,
		payment != "", site.FreePlan(), external})
}

var stripePlans = map[bool]map[string]string{
	true: { // Production
		"personal":     "plan_GLVKIvCvCjzT2u",
		"personalplus": "plan_GlJxixkxNZZOct",
		"business":     "plan_GLVGCVzLaPA3cY",
		"businessplus": "plan_GLVHJUi21iV4Wh",
		"donate":       "sku_H9jE6zFGzh6KKb",
	},
	false: { // Test data
		"personal":     "plan_GLWnXaogEns1n2",
		"personalplus": "plan_GlJvCPdCeUpww3",
		"business":     "plan_GLWoJ72fcNGoUD",
		"businessplus": "plan_GLWootweDZnKBk",
		"donate":       "sku_H9iAuKytd1eeN9",
	},
}

func (h billing) start(w http.ResponseWriter, r *http.Request) error {
	site := goatcounter.MustGetSite(r.Context())

	var args struct {
		Plan     string `json:"plan"`
		Quantity string `json:"quantity"`
	}
	_, err := zhttp.Decode(r, &args)
	if err != nil {
		return err
	}

	// Temporary log, since I got some JS "Bad Request" errors without any
	// detail which I can't reproduce :-/
	zlog.Fields(zlog.F{
		"args": args,
		"site": site.Code,
	}).Printf("billing/start")

	v := zvalidate.New()
	v.Required("plan", args.Plan)
	v.Include("plan", args.Plan, goatcounter.Plans)
	v.Required("quantity", args.Quantity)
	quantity := v.Integer("quantity", args.Quantity)
	if v.HasErrors() {
		return v
	}

	// Use dummy Stripe customer for personal plan without donations.
	if args.Plan == goatcounter.PlanPersonal && quantity == 0 {
		err := site.UpdateStripe(r.Context(),
			fmt.Sprintf("cus_free_%d", site.ID),
			goatcounter.PlanPersonal, "")
		if err != nil {
			return err
		}
		zhttp.Flash(w, "Saved!")
		return zhttp.JSON(w, "")
	}

	body := zstripe.Body{
		"mode":                                 "subscription",
		"payment_method_types[]":               "card",
		"client_reference_id":                  strconv.FormatInt(site.ID, 10),
		"success_url":                          site.URL() + "/billing?return=success",
		"cancel_url":                           site.URL() + "/billing?return=cancel",
		"subscription_data[items][][plan]":     stripePlans[cfg.Prod][args.Plan],
		"subscription_data[items][][quantity]": args.Quantity,
	}
	if site.Stripe != nil && !site.FreePlan() {
		body["customer"] = *site.Stripe
	} else {
		body["customer_email"] = goatcounter.GetUser(r.Context()).Email
	}

	var id zstripe.ID
	_, err = zstripe.Request(&id, "POST", "/v1/checkout/sessions", body.Encode())
	if err != nil {
		return errors.Errorf("zstripe failed: %w; body: %s", err, body.Encode())
	}

	return zhttp.JSON(w, id)
}

func (h billing) cancel(w http.ResponseWriter, r *http.Request) error {
	var customer struct {
		Subscriptions struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"subscriptions"`
	}

	site := goatcounter.MustGetSite(r.Context())
	if site.Stripe == nil {
		return guru.New(400, "No Stripe customer for this site?")
	}

	err := zdb.TX(r.Context(), func(ctx context.Context, db zdb.DB) error {
		err := site.UpdateStripe(r.Context(), *site.Stripe, goatcounter.PlanPersonal, "")
		if err != nil {
			return err
		}

		_, err = zstripe.Request(&customer, "GET",
			fmt.Sprintf("/v1/customers/%s", *site.Stripe), "")
		if err != nil {
			return err
		}

		if len(customer.Subscriptions.Data) == 0 {
			zhttp.FlashError(w, "No current subscriptions")
			return zhttp.SeeOther(w, "/billing")
		}
		if len(customer.Subscriptions.Data) > 1 {
			return errors.Errorf(
				"billing.cancel: unexpected number of subscriptions for site %d/%s",
				site.ID, *site.Stripe)
		}

		_, err = zstripe.Request(nil, "DELETE",
			fmt.Sprintf("/v1/subscriptions/%s", customer.Subscriptions.Data[0].ID),
			zstripe.Body{"prorate": "true"}.Encode())
		return err
	})
	if err != nil {
		return err
	}

	bgrun.Run(func() {
		blackmail.Send("GoatCounter cancellation",
			blackmail.From("GoatCounter Billing", "billing@goatcounter.com"),
			blackmail.To("billing@goatcounter.com"),
			blackmail.Bodyf(`Cancelled: %s (%d) %s`, site.Code, site.ID, *site.Stripe))
	})

	zhttp.Flash(w, "Plan cancelled; you will be refunded for the remaining period.")
	return zhttp.SeeOther(w, "/billing")
}

type Session struct {
	ClientReferenceID string `json:"client_reference_id"`
	Customer          string `json:"customer"`
	DisplayItems      []struct {
		Amount   int    `json:"amount"`
		Currency string `json:"currency"`
		Quantity int    `json:"quantity"`
		Plan     struct {
			ID string `json:"id"`
		} `json:"plan"`
	} `json:"display_items"`
}

// Test webhooks with the Stripe CLI:
//   ./stripe listen  --forward-to 'arp242.goatcounter.localhost:8081/stripe-webhook'
func (h billing) stripeWebhook(w http.ResponseWriter, r *http.Request) error {
	var event zstripe.Event
	err := event.Read(r)
	if err != nil {
		return err
	}

	switch event.Type {
	case "checkout.session.completed":
		var s Session
		err := json.Unmarshal(event.Data.Raw, &s)
		if err != nil {
			return err
		}

		if strings.HasPrefix(s.ClientReferenceID, "one-time") {
			bgrun.Run(func() {
				t := "New one-time donation: " + s.ClientReferenceID
				blackmail.Send(t,
					blackmail.From("GoatCounter Billing", "billing@goatcounter.com"),
					blackmail.To("billing@goatcounter.com"),
					blackmail.Bodyf(t))
			})
			return zhttp.String(w, "okay")
		}

		ctx := zdb.With(context.Background(), zdb.MustGet(r.Context()))
		bgrun.Run(func() {
			l := zlog.Module("billing").FieldsRequest(r).Field("session", s)
			id, err := strconv.ParseInt(s.ClientReferenceID, 10, 64)
			if err != nil {
				l.Error(err)
				return
			}

			var site goatcounter.Site
			err = site.ByID(ctx, id)
			if err != nil {
				l.Error(err)
				return
			}

			var plan string
			for name, p := range stripePlans[cfg.Prod] {
				if p == s.DisplayItems[0].Plan.ID {
					plan = name
				}
			}

			amount := fmt.Sprintf("%s %d", strings.ToUpper(s.DisplayItems[0].Currency),
				s.DisplayItems[0].Amount*s.DisplayItems[0].Quantity/100)
			err = site.UpdateStripe(ctx, s.Customer, plan, amount)
			if err != nil {
				l.Error(err)
				return
			}
		})
	}

	return zhttp.String(w, "okay")
}

func (h billing) confirmCancel(w http.ResponseWriter, r *http.Request) error {
	return zhttp.Template(w, "billing_cancel.gohtml", struct {
		Globals
	}{newGlobals(w, r)})
}
