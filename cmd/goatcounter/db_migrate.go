// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package main

import (
	"fmt"
	"strings"

	"zgo.at/errors"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/db/migrate/gomig"
	"zgo.at/zdb"
	"zgo.at/zli"
	"zgo.at/zlog"
	"zgo.at/zstd/zfs"
	"zgo.at/zstd/zstring"
)

func cmdDBMigrate(f zli.Flags, dbConnect, debug *string, createdb *bool) error {
	var dev = f.Bool(false, "dev").Pointer()
	err := f.Parse()
	if err != nil {
		return err
	}

	if len(f.Args) == 0 {
		return errors.New("need a migration or command")
	}

	return func(dbConnect, debug string, createdb, dev bool) error {
		zlog.Config.SetDebug(debug)

		db, _, err := connectDB(dbConnect, nil, createdb, false)
		if err != nil {
			return err
		}
		defer db.Close()

		fsys, err := zfs.EmbedOrDir(goatcounter.DB, "", dev)
		if err != nil {
			return err
		}
		m, err := zdb.NewMigrate(db, fsys, gomig.Migrations)
		if err != nil {
			return err
		}

		if zstring.ContainsAny(f.Args, "pending", "list") {
			have, ran, err := m.List()
			if err != nil {
				return err
			}
			diff := zstring.Difference(have, ran)
			pending := "no pending migrations"
			if len(diff) > 0 {
				pending = fmt.Sprintf("pending migrations:\n\t%s", strings.Join(diff, "\n\t"))
			}

			if zstring.Contains(f.Args, "list") {
				for i := range have {
					if zstring.Contains(diff, have[i]) {
						have[i] = "pending: " + have[i]
					}
				}
				fmt.Fprintln(zli.Stdout, strings.Join(have, "\n"))
				return nil
			}

			if len(diff) > 0 {
				return errors.New(pending)
			}
			fmt.Fprintln(zli.Stdout, pending)
			return nil
		}

		return m.Run(f.Args...)
	}(*dbConnect, *debug, *createdb, *dev)
}
