// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package main

import (
	"context"
	"fmt"
	"os"

	"zgo.at/errors"
	"zgo.at/goatcounter"
	"zgo.at/zdb"
	"zgo.at/zli"
	"zgo.at/zlog"
)

const helpDB = `
The db command allows managing the GoatCounter database.

Some common examples:

    Create a new site:

        $ goatcounter db create site -vhost stats.example.com -email martin@example.com

    Create a new API key:

        $ goatcounter db create apikey -site stats.example.com -perm count

    Run database migrations:

        $ goatcounter db migrate all

` + helpDBCommands + `

Flags accepted by all commands:

  -db          Database connection: "sqlite://<file>" or "postgres://<connect>"
               See "goatcounter help db" for detailed documentation. Default:
               sqlite://db/goatcounter.sqlite3?_busy_timeout=200&_journal_mode=wal&cache=shared

  -createdb    Create the database if it doesn't exist yet.

  -debug       Modules to debug, comma-separated or 'all' for all modules.
               See "goatcounter help debug" for a list of modules.

create, update, delete, and show commands:

    The "CRUD" commands

    The show, update, and delete commands accept a -find flag to find the rows
    to operate on. This always accepts the row's ID column, and can also accept
    a more friendly name. See below for the details on the table. This flag can
    be given more than once, in which case it will show, update, or delete
    multiple rows.

    The create and update commands accept a set of flags with column values
    which are documented below for the different tables. This isn't a full list
    of all columns, just the useful ones for regular management. create and
    update are identical, except that "update" needs a value for -find.

    Flags for "site":

        -find        Site to show, update, or delete. Can be as ID ("1") or
                     vhost ("stats.example.com").

        -vhost       Domain to host this site at (e.g. "stats.example.com"). The
                     site will be available on this domain only, so
                     "stats.example.com" won't be available on "localhost".

        -link        Link to this site; the site will use the same users, copies
                     this site's settings on creation, and will be listed in the
                     top navigation
                     Can be as ID ("1") or vhost ("stats.example.com").

        Only or "create", as a convenience to create a new user:

            -user.email       Your email address. Will be required to login.

            -user.password    Password to log in; will be asked interactively if omitted.

        Additional flags for "delete":

            -hard       Hard-delete a site. The default is to only "soft-delete"
                        a site, maintaining all data, and allows recovering the
                        site. After seven days it will be pernamently deleted.
                        With this option it will immediatly delete the site and
                        all associated data. This may take a minute if you have
                        a lot of data.

            -force      Force deletion. If there are sites linked to this
                        account then it will also delete all those sites.

    Flags for "user":

        -find       ID or email

        -site       Site to use; as ID or vhost.

        -email      Email address.

        -password   Password

        Additional flags for "delete":

            -hard       Hard-delete a user.

            -force      Force deletion even if this is the last admin user.

    Flags for "apikey":

        -find        API token to update. Can be as ID ("1") or token ("af41...").

        -site        Site to create API key for.

        -perm        Comma-separated list of permissions to assign; possible
                     values:

                        count
                        export
                        site_read
                        site_create
                        site_update

newdb command:

    Create a new database. This is the same what "goatcounter serve" or
    "goatcounter db -createdb [command]" does.

migrate command:

    Run or print database migrations.

    -dev         Load migrations from filesystem, rather than using the migrations
                compiled in the binary.

    Positional arguments are names of the migration, either as just the name
    ("2020-01-05-2-x") or as the file path ("./db/migrate/2020-01-05-2-x.sql").

    Special values:

        all         Run all pending migrations.
        pending     Show pending migrations but do not run anything. Exits with 1 if
                    there are pending migrations, or 0 if there aren't.
        list        List all migrations; pending migrations are prefixed with
                    "pending: ". Always exits with 0.

    Note: you can also use -automigrate flag for the serve command to run migrations
    on startup.

schema-sqlite and schema-pgsql commands:

    Print the compiled-in database schema for SQLite or PostgreSQL.

test command:

    Test if the database exists; exits with 0 on success, 2 if the database
    doesn't exist, and 1 on any other error.

    This is useful for setting up new databases in scripts if you don't want to
    use the default database creation; e.g.:

        goatcounter db test -db [..]
        if [ $? -eq 2 ]; then
            createdb goatcounter
            goatcounter db schema-pgsql | psql goatcounter
        fi

query command:

    TODO

Detailed documentation on the -db flag:

    GoatCounter can use SQLite and PostgreSQL. All commands accept the -db flag
    to customize the database connection string.

    You can select a database engine by using "sqlite://[..]" for SQLite, or
    "postgresql://[..]" (or "postgres://[..]") for PostgreSQL.

    There are no plans to support other database engines such as MySQL/MariaDB.

    The database is automatically created for the "serve" command, but you need
    to add -createdb to any other commands to create the database. This is to
    prevent accidentally operating on the wrong (new) database.

SQLite notes:

    This is the default database engine as it has no dependencies, and for most
    small to medium usage it should be more than fast enough.

    The SQLite connection string is usually just a filename, optionally prefixed
    with "file:". Parameters can be added as a URL query string after a ?:

        -db 'sqlite://mydb.sqlite?param=value&other=value'

    See the go-sqlite3 documentation for a list of supported parameters:
    https://github.com/mattn/go-sqlite3/#connection-string

    _journal_mode=wal is always added unless explicitly overridden. Usually the
    Write Ahead Log is more suitable for GoatCounter than the default DELETE
    journaling.

PostgreSQL notes:

    PostgreSQL provides better performance for large instances. If you have
    millions of pageviews then PostgreSQL is probably a better choice.

    The PostgreSQL connection string can either be as "key=value" or as an URL;
    the following are identical:

        -db 'postgresql://user=pqgotest dbname=pqgotest sslmode=verify-full'
        -db 'postgresql://pqgotest:password@localhost/pqgotest?sslmode=verify-full'

    See the pq documentation for a list of supported parameters:
    https://pkg.go.dev/github.com/lib/pq?tab=doc#hdr-Connection_String_Parameters

    You can also use the standard PG* environment variables:

        PGDATABASE=goatcounter DBHOST=/var/run goatcounter -db 'postgresql://'

    You may want to consider lowering the "seq_page_cost" parameter; the query
    planner tends to prefer seq scans instead of index scans for some operations
    with the default of 4, which is much slower. I found that 0.5 is a fairly
    good setting, you can set it in your postgresql.conf file, or just for one
    database with:

        alter database goatcounter set seq_page_cost=.5
`

const helpDBCommands = `List of commands:

     create [table]     Create a new row.
     update [table]     Update a row.
     delete [table]     Delete a row.
     show   [table]     Show a row.

                        Valid tables are "site", "user", and "apikey".

     newdb              Create a new database.
     migrate            Run or view database migrations.
     schema-sqlite      Print the SQLite schema.
     schema-pgsql       Print the PostgreSQL schema.
     test               Test if the database exists
     query              Run a query`

const helpDBShort = "\n" + helpDBCommands + `

Use "goatcounter help db" for the full documentation.`

func cmdDB(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error {
	defer func() { ready <- struct{}{} }()

	var (
		dbConnect = f.String("sqlite://db/goatcounter.sqlite3", "db").Pointer()
		debug     = f.String("", "debug").Pointer()
		createdb  = f.Bool(false, "createdb").Pointer()
	)

	// TODO: "goatcounter -db=foo db query" doesn't work. Flags should be
	// allowed in any position.
	cmd := f.Shift()
	switch cmd {
	default:
		return errors.Errorf("unknown command for \"db\": %q\n%s", cmd, helpDBShort)
	case "":
		return errors.New("\"db\" needs a subcommand\n" + helpDBShort)
	case "help":
		zli.WantColor = true
		printHelp(helpDB)
		return nil

	case "schema-sqlite", "schema-pgsql":
		return cmdDBSchema(cmd)

	case "test":
		err := f.Parse()
		if err != nil {
			return err
		}
		return cmdDBTest(*dbConnect, *debug)

	case "newdb":
		err := f.Parse()
		if err != nil {
			return err
		}

		db, _, err := connectDB(*dbConnect, []string{"pending"}, true, false)
		if err != nil {
			return err
		}
		db.Close()

	case "migrate":
		err := cmdDBMigrate(f, dbConnect, debug, createdb)
		if err != nil {
			return err
		}

	case "query":
		var format = f.String("", "format")
		err := f.Parse()
		if err != nil {
			return err
		}
		if len(f.Args) != 1 {
			return errors.New("must give exactly one parameter")
		}

		var params []interface{}
		switch format.String() {
		case "vertical":
			params = append(params, zdb.DumpVertical)
		case "csv":
			params = append(params, zdb.DumpCSV)
		case "json":
			params = append(params, zdb.DumpJSON)
		case "":
		default:
			return fmt.Errorf("-format: unknown value: %q", format.String())
		}

		db, ctx, err := connectDB(*dbConnect, []string{"pending"}, *createdb, false)
		if err != nil {
			return err
		}
		defer db.Close()

		q, err := db.Load(ctx, "db.query."+f.Args[0]+".sql")
		if err != nil {
			fmt.Println(err)
			q = f.Args[0]
		}

		zdb.Dump(ctx, os.Stdout, q, params...)

	case "create", "update", "delete", "show":
		tbl := f.Shift()
		var fun func(f zli.Flags, cmd string, dbConnect, debug *string, createdb *bool) error
		switch tbl {
		default:
			return errors.Errorf("unknown table %q\n%s", tbl, helpDBShort)
		case "":
			return errors.Errorf("%q commands needs a table name\n%s", cmd, helpDBShort)
		case "help":
			zli.WantColor = true
			printHelp(helpDB)
			return nil

		case "site", "sites":
			fun = cmdDBSite
		case "user", "users":
			// fun = cmdDBUser
		case "apikey", "apikeys":
			// fun = cmdDBAPIKey
		}

		return fun(f, cmd, dbConnect, debug, createdb)
	}
	return nil
}

func cmdDBSchema(cmd string) error {
	d, err := goatcounter.DB.ReadFile("db/schema.gotxt")
	if err != nil {
		return err
	}
	driver := zdb.DriverSQLite
	if cmd == "schema-pgsql" {
		driver = zdb.DriverPostgreSQL
	}
	d, err = zdb.SchemaTemplate(driver, string(d))
	if err != nil {
		return err
	}
	fmt.Fprint(zli.Stdout, string(d))
	return nil
}

func cmdDBTest(dbConnect, debug string) error {
	if dbConnect != "" {
		return errors.New("must add -db flag")
	}
	zlog.Config.SetDebug(debug)
	db, err := zdb.Connect(zdb.ConnectOptions{Connect: dbConnect})
	if err != nil {
		return err
	}
	defer db.Close()

	var i int
	err = db.Get(context.Background(), &i, `select 1 from version`)
	if err != nil {
		return fmt.Errorf("select 1 from version: %w", err)
	}
	fmt.Fprintf(zli.Stdout, "DB at %q seems okay\n", dbConnect)
	return nil
}
