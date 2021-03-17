// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package goatcounter

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	"zgo.at/json"
	"zgo.at/tz"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/zjson"
)

// Settings.Collect values (bitmask)
// Note: also update CollectFlags() method below.
const (
	_                     zint.Bitflag16 = 1 << iota // Skip 1 so we can pass that from tests as "empty".
	CollectReferrer                                  // 2
	CollectUserAgent                                 // 4
	CollectScreenSize                                // 8
	CollectLocation                                  // 16
	CollectLocationRegion                            // 32
	CollectLanguage                                  // 64
	CollectSession                                   // 128
)

type (
	// SiteSettings contains all the user-configurable settings for a site, with
	// the exception of the domain and billing settings.
	//
	// This is stored as JSON in the database.
	SiteSettings struct {
		// Global site settings.

		Public        bool           `json:"public"`
		AllowCounter  bool           `json:"allow_counter"`
		AllowAdmin    bool           `json:"allow_admin"`
		DataRetention int            `json:"data_retention"`
		Campaigns     Strings        `json:"campaigns"`
		IgnoreIPs     Strings        `json:"ignore_ips"`
		Collect       zint.Bitflag16 `json:"collect"`

		// User preferences.

		TwentyFourHours  bool     `json:"twenty_four_hours"`
		SundayStartsWeek bool     `json:"sunday_starts_week"`
		DateFormat       string   `json:"date_format"`
		NumberFormat     rune     `json:"number_format"`
		Timezone         *tz.Zone `json:"timezone"`
		Widgets          Widgets  `json:"widgets"`
		Views            Views    `json:"views"`
	}

	// Widgets is a list of widgets to be printed, in order.
	Widgets []Widget
	Widget  map[string]interface{}

	widgetSettings map[string]widgetSetting
	widgetSetting  struct {
		Type  string
		Label string
		Help  string

		Value interface{}
	}

	// Views for the dashboard; these settings apply to all widget and are
	// configurable in the yellow box at the top.
	Views []View
	View  struct {
		Name   string `json:"name"`
		Filter string `json:"filter"`
		Daily  bool   `json:"daily"`
		AsText bool   `json:"as-text"`
		Period string `json:"period"` // "week", "week-cur", or n days: "8"
	}
)

// Default widgets for new sites.
//
// This *must* return a list of all configurable widgets; even if it's off by
// default.
//
// As a function to ensure a global map isn't accidentally modified.
func defaultWidgets() Widgets {
	s := defaultWidgetSettings()
	w := Widgets{}
	for _, n := range []string{"pages", "totalpages", "toprefs", "browsers", "systems", "sizes", "locations"} {
		w = append(w, map[string]interface{}{"on": true, "name": n, "s": s[n].getMap()})
	}
	return w
}

// List of all settings for widgets with some data.
func defaultWidgetSettings() map[string]widgetSettings {
	return map[string]widgetSettings{
		"pages": map[string]widgetSetting{
			"limit_pages": widgetSetting{
				Type:  "number",
				Label: "Page size",
				Help:  "Number of pages to load",
				Value: float64(10),
			},
			"limit_refs": widgetSetting{
				Type:  "number",
				Label: "Referrers page size",
				Help:  "Number of referrers to load when clicking on a path",
				Value: float64(10),
			},
		},
		"totalpages": map[string]widgetSetting{
			"align": widgetSetting{
				Type:  "checkbox",
				Label: "Align with pages",
				Help:  "Add margin to the left so it aligns with pages charts",
				Value: false,
			},
			"no-events": widgetSetting{
				Type:  "checkbox",
				Label: "Exclude events",
				Help:  "Don't include events in the Totals overview",
				Value: false,
			},
		},
	}
}

func (ss SiteSettings) String() string { return string(zjson.MustMarshal(ss)) }

// Value implements the SQL Value function to determine what to store in the DB.
func (ss SiteSettings) Value() (driver.Value, error) { return json.Marshal(ss) }

// Scan converts the data returned from the DB into the struct.
func (ss *SiteSettings) Scan(v interface{}) error {
	switch vv := v.(type) {
	case []byte:
		return json.Unmarshal(vv, ss)
	case string:
		return json.Unmarshal([]byte(vv), ss)
	default:
		return fmt.Errorf("SiteSettings.Scan: unsupported type: %T", v)
	}
}

func (ss *SiteSettings) Defaults() {
	if ss.DateFormat == "" {
		ss.DateFormat = "2 Jan ’06"
	}
	if ss.NumberFormat == 0 {
		ss.NumberFormat = 0x202f
	}
	if ss.Timezone == nil {
		ss.Timezone = tz.UTC
	}
	if ss.Campaigns == nil {
		ss.Campaigns = []string{"utm_campaign", "utm_source", "ref"}
	}

	if len(ss.Widgets) == 0 {
		ss.Widgets = defaultWidgets()
	}
	if len(ss.Views) == 0 {
		ss.Views = Views{{Name: "default", Period: "week"}}
	}
	if ss.Collect == 0 {
		ss.Collect = CollectReferrer | CollectUserAgent | CollectScreenSize | CollectLocation | CollectLocationRegion | CollectSession
	}
	// Collecting region without country makes no sense.
	if ss.Collect.Has(CollectLocationRegion) {
		ss.Collect |= CollectLocation
	}
}

type CollectFlag struct {
	Label, Help string
	Flag        zint.Bitflag16
}

// CollectFlags returns a list of all flags we know for the Collect settings.
//func (ss SiteSettings) CollectFlags() []zint.Bitflag8 {
func (ss SiteSettings) CollectFlags() []CollectFlag {
	return []CollectFlag{
		{
			Label: "Sessions",
			Help:  "Identify unique visitors",
			Flag:  CollectSession,
		},
		{
			Label: "Referrer",
			Help:  "Referrer header and campaign parameters",
			Flag:  CollectReferrer,
		},
		{
			Label: "User-Agent",
			Help:  "Browser and OS from User-Agent",
			Flag:  CollectUserAgent,
		},
		{
			Label: "Size",
			Help:  "Screen size",
			Flag:  CollectScreenSize,
		},
		{
			Label: "Country",
			Help:  "Country name (i.e. Belgium, Indonesia, etc.)",
			Flag:  CollectLocation,
		},
		{
			Label: "Region",
			Help:  "Region (i.e. Texas, Bali, etc.)",
			Flag:  CollectLocationRegion,
		},
		// {
		// 	Label: "Language",
		// 	Help:  "Supported languages from Accept-Language",
		// 	Flag:  CollectLanguage,
		// },
	}
}

func (s widgetSettings) getMap() map[string]interface{} {
	m := make(map[string]interface{})
	for k, v := range s {
		m[k] = v.Value
	}
	return m
}

// SetSettings set the setting "setting" for widget "widget" to "value".
//
// The value is converted to the correct type for this setting.
func (w Widget) SetSetting(widget, setting, value string) error {
	defW, ok := defaultWidgetSettings()[widget]
	if !ok {
		return fmt.Errorf("Widget.SetSetting: no such widget %q", widget)
	}
	def, ok := defW[setting]
	if !ok {
		return fmt.Errorf("Widget.SetSetting: no such setting %q for widget %q", setting, widget)
	}

	s, ok := w["s"].(map[string]interface{})
	if !ok {
		s = make(map[string]interface{})
	}
	switch def.Type {
	case "number":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		s[setting] = float64(n)
	case "checkbox":
		s[setting] = value == "on"
	case "text":
		s[setting] = value
	}
	w["s"] = s
	return nil
}

// Get a widget from the list by name.
func (w Widgets) Get(name string) Widget {
	for _, v := range w {
		if v["name"] == name {
			return v
		}
	}
	return nil
}

// GetSettings gets all setting for this widget.
func (w Widgets) GetSettings(name string) widgetSettings {
	for _, v := range w {
		if v["name"] == name {
			def := defaultWidgetSettings()[name]
			s, ok := v["s"]
			if ok {
				// {"limit_pages": 10, "limit_refs": 10}},
				ss := s.(map[string]interface{})
				for k, v := range ss {
					if v != nil {
						d := def[k]
						d.Value = v
						def[k] = d
					}
				}
			}
			return def
		}
	}
	return make(widgetSettings)
}

// On reports if this setting should be displayed.
func (w Widgets) On(name string) bool {
	ww := w.Get(name)
	b, ok := ww["on"].(bool)
	if !ok {
		return false
	}
	return b
}

// Get a view for this site by name and returns the view and index.
// Returns -1 if this view doesn't exist.
func (v Views) Get(name string) (View, int) {
	for i, vv := range v {
		if strings.EqualFold(vv.Name, name) {
			return vv, i
		}
	}
	return View{}, -1
}

// Some shortcuts for getting the settings.

func (ss SiteSettings) LimitPages() int {
	return int(ss.Widgets.GetSettings("pages")["limit_pages"].Value.(float64))
}
func (ss SiteSettings) LimitRefs() int {
	return int(ss.Widgets.GetSettings("pages")["limit_refs"].Value.(float64))
}
func (ss SiteSettings) SplitEvents() bool {
	return ss.Widgets.GetSettings("pages")["split_events"].Value.(bool)
}
func (ss SiteSettings) TotalsAlign() bool {
	return ss.Widgets.GetSettings("totalpages")["align"].Value.(bool)
}
func (ss SiteSettings) TotalsNoEvents() bool {
	return ss.Widgets.GetSettings("totalpages")["no-events"].Value.(bool)
}
