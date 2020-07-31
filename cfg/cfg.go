// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

// Package cfg contains global application configuration settings.
package cfg

// Configuration variables.
var (
	Domain         string
	DomainStatic   string
	DomainCount    string
	URLStatic      string
	PgSQL          bool
	Plan           string
	Prod           bool
	Version        string
	GoatcounterCom bool
	Serve          bool
	Port           string
	EmailFrom      string
	Memcached      string

	RunningTests bool
)
