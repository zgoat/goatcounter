// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	_ "time/tzdata"

	_ "github.com/lib/pq"           // PostgreSQL database driver.
	_ "github.com/mattn/go-sqlite3" // SQLite database driver.
	"zgo.at/errors"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/db/migrate/gomig"
	"zgo.at/zdb"
	"zgo.at/zli"
	"zgo.at/zlog"
	"zgo.at/zstd/zfs"
	"zgo.at/zstd/zruntime"
	"zgo.at/zstd/zstring"
)

func init() {
	errors.Package = "zgo.at/goatcounter"
}

type command func(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error

func main() {
	var (
		f     = zli.NewFlags(os.Args)
		ready = make(chan struct{}, 1)
		stop  = make(chan struct{}, 1)
	)
	cmdMain(f, ready, stop)
}

var mainDone sync.WaitGroup

func cmdMain(f zli.Flags, ready chan<- struct{}, stop chan struct{}) {
	mainDone.Add(1)
	defer mainDone.Done()

	cmd := f.Shift()
	if zstring.ContainsAny(f.Args, "-h", "-help", "--help") {
		f.Args = append([]string{cmd}, f.Args...)
		cmd = "help"
	}

	var run command
	switch cmd {
	default:
		zli.Errorf(usage[""])
		zli.Errorf("unknown command: %q", cmd)
		zli.Exit(1)
		return
	case "", "help":
		run = cmdHelp
	case "version":
		fmt.Fprintln(zli.Stdout, getVersion())
		zli.Exit(0)
		return

	case "db", "database":
		run = cmdDb
	case "create":
		run = cmdCreate
	case "migrate":
		run = cmdMigrate
	case "reindex":
		run = cmdReindex
	case "serve":
		run = cmdServe
	case "saas":
		run = cmdSaas
	case "monitor":
		run = cmdMonitor
	case "import":
		run = cmdImport
	case "buffer":
		run = cmdBuffer
	}

	err := run(f, ready, stop)
	if err != nil {
		zli.Errorf(err)
		zli.Exit(1)
		return
	}
	zli.Exit(0)
}

func connectDB(connect string, migrate []string, create, dev bool) (zdb.DB, context.Context, error) {
	fsys, err := zfs.EmbedOrDir(goatcounter.DB, "db", dev)
	if err != nil {
		return nil, nil, err
	}

	db, err := zdb.Connect(zdb.ConnectOptions{
		Connect:      connect,
		Files:        fsys,
		Migrate:      migrate,
		GoMigrations: gomig.Migrations,
		Create:       create,
		SQLiteHook:   goatcounter.SQLiteHook,
		MigrateLog:   func(name string) { zlog.Printf("ran migration %q", name) },
	})
	var pErr *zdb.PendingMigrationsError
	if errors.As(err, &pErr) {
		zlog.Errorf("%s; continuing but things may be broken", err)
		err = nil
	}
	return db, goatcounter.NewContext(db), err
}

func getVersion() string {
	return fmt.Sprintf("version=%s; go=%s; GOOS=%s; GOARCH=%s; race=%t; cgo=%t",
		goatcounter.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH,
		zruntime.Race, zruntime.CGO)
}
