// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package main

import (
	"strings"
	"testing"
)

func TestDb(t *testing.T) {
	exit, _, out, _, dbc := startTest(t)

	{
		runCmd(t, exit, "db", "schema-sqlite")
		wantExit(t, exit, out, 0)
		if len(out.String()) < 1_000 {
			t.Error()
		}
		out.Reset()
	}

	{
		runCmd(t, exit, "db", "schema-pgsql")
		wantExit(t, exit, out, 0)
		if len(out.String()) < 1_000 {
			t.Error()
		}
		out.Reset()
	}

	{
		runCmd(t, exit, "db", "test", "-db="+dbc)
		wantExit(t, exit, out, 0)
		if out.String() != "DB seems okay\n" {
			t.Error()
		}
		out.Reset()
	}

	{
		runCmd(t, exit, "db", "test", "-db=sqlite://yeah_nah_doesnt_exist")
		wantExit(t, exit, out, 1)
		if !strings.Contains(out.String(), `doesn't exist`) {
			t.Error(out.String())
		}
		out.Reset()
	}
}
