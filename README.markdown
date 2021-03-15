[![Awesome Humane Tech](https://raw.githubusercontent.com/humanetech-community/awesome-humane-tech/main/humane-tech-badge.svg?sanitize=true)](https://github.com/humanetech-community/awesome-humane-tech)

GoatCounter is an open source web analytics platform available as a hosted
service (free for non-commercial use) or self-hosted app. It aims to offer easy
to use and meaningful privacy-friendly web analytics as an alternative to Google
Analytics or Matomo.

There are two ways to run this: as **hosted service** on [goatcounter.com][www],
*free* for non-commercial use, or run it on your own server. The source code is
completely Open Source/Free Software, and it can be self-hosted without
restrictions.

See [docs/rationale.markdown](docs/rationale.markdown) for some more details on
the *"why?"* of this project.

There's a live demo at [https://stats2.arp242.net](https://stats2.arp242.net).

Please consider [contributing financially][sponsor] if you're self-hosting
GoatCounter so I can pay my rent :-) GoatCounter is sponsored by a grant from
[NLnet's NGI Zero PET fund][nlnet].

[nlnet]: https://nlnet.nl/project/GoatCounter/
[sponsor]: http://www.goatcounter.com/contribute
[www]: https://www.goatcounter.com


Features
--------

- **Privacy-aware**; doesn’t track users with unique identifiers and doesn't
  need a GDPR notice. Fine-grained **control over which data is collected**.
  Also see the [privacy policy][privacy] and [GDPR consent notices][gdpr].

- **Lightweight** and **fast**; adds just ~3.5K of extra data to your site. Also
  has JavaScript-free "tracking pixel" option, or you can use it from your
  application's middleware or **import from logfiles**.

- Identify **unique visits** without cookies using a non-identifiable hash
  ([technical details][sessions]).

- Keeps useful statistics such as **browser** information, **location**, and
  **screen size**. Keep track of **referring sites** and **campaigns**.

- **Easy**; if you've been confused by the myriad of options and flexibility of
  Google Analytics and Matomo that you don't need then GoatCounter will be a
  breath of fresh air.

- **Accessibility** is a high-priority feature, and the interface works well
  with assistive technology such as screen readers.

- 100% committed to **open source**; you can see exactly what the code does and
  make improvements, or <strong>self-host</strong> it for any purpose.

- **Own your data**; you can always export all data and **cancel at any time**.

- Integrate on your site with just a **single script tag**:

      <script data-goatcounter="https://yoursite.goatcounter.com/count"
              async src="//gc.zgo.at/count.js"></script>

- The JavaScript integration is a good option for most, but you can also use a
  **no-JavaScript image-based tracker**, integrate it in your **backend
  middleware**, or **parse log files**.

[privacy]: https://www.goatcounter.com/privacy
[gdpr]: https://www.goatcounter.com/gdpr
[sessions]: https://github.com/zgoat/goatcounter/blob/master/docs/sessions.markdown


Getting data in to GoatCounter
------------------------------

There are three ways:

1. Add the JavaScript code on your site; this is the easiest and most common
   method. Detailed documentation for this is available at
   https://www.goatcounter.com/code

2. Integrate in your middleware; send data to GoatCounter by calling the API
   from your backend server middleware. Detailed documentation for this is
   available at https://www.goatcounter.com/api#backend-integration

3. Parse logfiles. GoatCounter can parse logfiles from nginx, Apache,
   CloudFront, or any other HTTP middleware or proxy. See `goatcounter help
   import` for detailed documentation on this.


Running your own
----------------

The [release page][releases] has binaries for Linux amd64, arm, and arm64. These
are statically compiled, contain everything you need, and should work in pretty
much any Linux environment. The only other thing you need is somewhere to store
a SQLite database file or a PostgreSQL connection.

GoatCounter should run on any platform supported by Go, but there are no
binaries for them (yet); you'll have to build from source if you want to run it
on e.g. FreeBSD or macOS.

Note this README is for the latest master; use the [`release-1.4`][r-1.4] branch
for the 1.4 README.

Generally speaking only the latest release is supported, although critical fixes
(security, data loss, etc.) may get backported to previous releases.

[releases]: https://github.com/zgoat/goatcounter/releases
[r-1.4]: https://github.com/zgoat/goatcounter/tree/release-1.4

### Deploy scripts and such

- ["StackScript" for Linode][stackscript]; Alpine Linux VPS; you can also use
  this for other Alpine Linux machines.

  If you don't have a Linode account yet then consider using my [referral
  URL][linode] and I'll get some kickback from Linode :-)

  [stackscript]: https://cloud.linode.com/stackscripts/659823
  [linode]: https://www.linode.com/?r=7acaf75737436d859e785dd5c9abe1ae99b4387e

- Some people have created Dockerfiles. You don't really need Docker since
  GoatCounter has no external dependencies; it probably [creates more problems
  than it solves][docker] IMHO. At any rate, here are some that seem alright at
  a glance if you must:

  - https://github.com/baethon/docker-goatcounter (https://hub.docker.com/r/baethon/goatcounter)
  - https://github.com/sent-hil/dokku-gocounter
  - https://github.com/anarcat/goatcounter/blob/Dockerfile/Dockerfile

  [docker]: https://www.youtube.com/watch?v=PivpCKEiQOQ

- Some other guides people have written:
  - [Replacing Google Analytics with GoatCounter](https://rgth.co/blog/replacing-google-analytics-with-goatcounter/) (Ubuntu)
  - [GoatCounter self-hosted setup on a VPS](https://actually.fyi/posts/goatcounter-vps/) (Arch Linux)
  - [GoatCounter server setup on OpenBSD](https://daulton.ca/2021/01/openbsd-goatcounter-server/)


### Building from source

Compile from source with:

    $ git clone -b release-1.4 https://github.com/zgoat/goatcounter.git
    $ cd goatcounter
    $ go build -ldflags="-X zgo.at/goatcounter.Version=$(git log -n1 --format='%h_%cI')" ./cmd/goatcounter

You'll now have a `goatcounter` binary in the current directory.

The `-ldflags=[..]` sets the version; this isn't *strictly* required as such,
but it's recommended as it's used to "bust" the cache for static files and may
also be useful later when reporting bugs. This can be any string and doesn't
follow any particular format, you can also set this to the current date or
`banana` or anything you want really.

To build a fully statically linked binary:

    $ go build -tags osusergo,netgo,sqlite_omit_load_extension \
        -ldflags="-X zgo.at/goatcounter.Version=$(git log -n1 --format='%h_%cI') -extldflags=-static" \
        ./cmd/goatcounter

You need Go 1.16 or newer and a C compiler (for SQLite). If you compile it with
`CGO_ENABLED=0 go build` you don't need a C compiler, but can only use PostgreSQL.

It's recommended to use the latest release as in the above command. The master
branch should be reasonably stable but no guarantees, and sometimes I don't
write detailed release/upgrade notes until the actual release so you may run in
to surprises.

### Running

You can start a server with:

    $ goatcounter serve

The default is to use an SQLite database at `./db/goatcounter.sqlite3`, which
will be created if it doesn't exist yet. See the `-db` flag and `goatcounter
help db` to customize this.

GoatCounter will listen on port `*:80` and `*:443` by default. You don't need
to run it as root and can grant the appropriate permissions on Linux with:

    $ setcap 'cap_net_bind_service=+ep' goatcounter

Listening on a different port can be a bit tricky due to the ACME/Let's Encrypt
certificate generation; `goatcounter help listen` documents this in depth.

You can create new sites with the `db create site` command:

    $ goatcounter db create site -email me@example.com -domain stats.example.com

This will ask for a password for your new account; you can also add a password
on the commandline with `-password`. You must also pass the `-db` flag here if
you use something other than the default.

### Updating

You may need to run the database migrations when updating. Use  `goatcounter
serve -automigrate` to always run all pending migrations on startup. This is the
easiest way, although arguably not the "best" way.

Use `goatcounter migrate <file>` or `goatcounter migrate all` to manually run
migrations; generally you want to upload the new version, run migrations while
the old one is still running, and then restart so the new version takes effect.

Use `goatcounter migrate pending` to get a list of pending migrations, or
`goatcounter migrate list` to show all migrations.

### PostgreSQL

Both SQLite and PostgreSQL are supported. SQLite should work well for most
smaller sites, but PostgreSQL gives some better performance:

1. Run with custom `-db` flag:

       $ goatcounter serve -db 'postgresql://dbname=goatcounter'

   Or use a socket:

       $ goatcounter serve \
           -db 'postgresql://host=/run/postgresql dbname=goatcounter sslmode=disable'

   You can also use the `PG*` environment variables:

       $ PGDATABASE=goatcounter DBHOST=/var/run goatcounter serve -db 'postgresql://'

   See `goatcounter help db` and the [pq docs][pq] for more details.

2. You can compile goatcounter without cgo if you don't use SQLite:

       $ CGO_ENABLED=0 go build -ldflags="-X zgo.at/goatcounter.Version=$(git log -n1 --format='%h_%cI')" ./cmd/goatcounter

   Functionally it doesn't matter too much, but builds will be a bit easier and
   faster as it won't require a C compiler.

[pq]: https://pkg.go.dev/github.com/lib/pq#hdr-Connection_String_Parameters

### Development/testing

You can start a test/development server with:

    $ goatcounter serve -dev

The `-dev` flag makes some small things a bit more convenient for development;
TLS is disabled by default, it will listen on localhost:8081, the application
will automatically restart on recompiles, templates and static files will be
read directly from the filesystem, and a few other minor changes.

See [.github/CONTRIBUTING.markdown](/.github/CONTRIBUTING.markdown) for more
details on how to run a development server, write patches, etc.

Various aggregate data files are available at https://www.goatcounter.com/data
