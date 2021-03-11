[![Awesome Humane Tech](https://raw.githubusercontent.com/humanetech-community/awesome-humane-tech/main/humane-tech-badge.svg?sanitize=true)](https://github.com/humanetech-community/awesome-humane-tech)

GoatCounter is an open source web analytics platform available as a hosted
service (free for non-commercial use) or self-hosted app. It aims to offer easy
to use and meaningful privacy-friendly web analytics as an alternative to Google
Analytics or Matomo.

There are two ways to run this: as **hosted service** on [goatcounter.com][www],
*free* for non-commercial use, or run it on your own server (the source code is
completely Open Source/Free Software, and it can be self-hosted without
restrictions).

See [docs/rationale.markdown](docs/rationale.markdown) for some more details on
the *"why?"* of this project.

There's a live demo at [https://stats2.arp242.net](https://stats2.arp242.net).

Please consider [contributing financially][sponsor] if you're self-hosting
GoatCounter so I can pay my rent :-)

GoatCounter is sponsored by a grant from [NLnet's NGI Zero PET fund][nlnet].

[nlnet]: https://nlnet.nl/project/GoatCounter/
[sponsor]: http://www.goatcounter.com/contribute
[www]: https://www.goatcounter.com

Features
--------

- **Privacy-aware**; doesn't track users with unique identifiers and doesn't
  need a GDPR consent notice. Also see the [privacy policy][privacy].

- **Lightweight** and **fast**; adds just ~3.5K of extra data to your site. Also
  has JavaScript-free "tracking pixel" option, or you can use it from your
  application's middleware.

- **Easy**; if you've been confused by the myriad of options and flexibility of
  Google Analytics and Matomo that you don't need then GoatCounter will be a
  breath of fresh air.

- Identify **unique visits** without cookies using a non-identifiable hash
  ([technical details][sessions]).

- Keeps useful statistics such as **browser** information, **location**, and
  **screen size**. Keep track of **referring sites** and **campaigns**.

- **Accessibility** is a high-priority feature, and the interface works well
  with screen readers, no JavaScript, and even text browsers (although not all
  features work equally well without JS).

- 100% committed to **open source**; you can see exactly what the code does and
  make improvements.

- **Own your data**; you can always export all data and **cancel at any time**.

- Integrate on your site with just a **single script tag**:

      <script data-goatcounter="https://yoursite.goatcounter.com/count"
              async src="//gc.zgo.at/count.js"></script>

- The JavaScript integration is a good option for most, but you can also use a
  **no-JavaScript image-based tracker**, integrate it in your **backend
  middleware**, or **parse log files**.

[privacy]: https://www.goatcounter.com/privacy
[sessions]: https://github.com/zgoat/goatcounter/blob/master/docs/sessions.markdown

### Technical

- Fast: can handle about 800 hits/second on a $5/month Linode VPS using the
  default settings.

- Self-contained binary: everything – including static assets – is in a single
  ~7M statically compiled binary. The only other thing you need is a SQLite
  database file or PostgreSQL connection (no way around that).

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
are statically compiled and should work in pretty much any Linux environment.
GoatCounter should run on any platform supported by Go, but there are no
binaries for them (yet); you'll have to build from source for now (it's not
hard, I promise).

Note this README is for the latest master; use the [`release-1.5`][r-1.5] branch
for the 1.5 README.

Generally speaking only the latest release is supported, although critical fixes
(security, data loss, etc.) may get backported to previous releases.

[releases]: https://github.com/zgoat/goatcounter/releases
[r-1.5]: https://github.com/zgoat/goatcounter/tree/release-1.5

### Deploy scripts and such

- ["StackScript" for Linode][stackscript]; you can also use this for other
  Alpine Linux machines.

  A $5/month Linode is more than enough to run GoatCounter unless you've got
  millions of pageviews. And if you don't have a Linode account yet then
  consider using my [referral URL][linode] and I'll get some cash back from
  Linode :-)

  [stackscript]: https://cloud.linode.com/stackscripts/659823
  [linode]: https://www.linode.com/?r=7acaf75737436d859e785dd5c9abe1ae99b4387e

- Some people have created Dockerfiles. You don't really need Docker since
  GoatCounter is a static binary with no external dependencies; it probably
  [creates more problems than it solves][docker] IMHO. At any rate, here are
  some that seem alright at a glance if you must:

  - https://github.com/baethon/docker-goatcounter (https://hub.docker.com/r/baethon/goatcounter)
  - https://github.com/sent-hil/dokku-gocounter
  - https://github.com/anarcat/goatcounter/blob/Dockerfile/Dockerfile

  [docker]: https://www.youtube.com/watch?v=PivpCKEiQOQ

### Building from source

Compile from source with:

    $ git clone -b release-1.5 https://github.com/zgoat/goatcounter.git
    $ cd goatcounter
    $ go build -ldflags="-X main.version=$(git log -n1 --format='%h_%cI')" ./cmd/goatcounter

The `-ldflags=[..]` sets the version; this isn't *strictly* required as such,
but it's recommended as it's used to "bust" the cache for static files and may
also be useful later when reporting bugs. This can be any string and doesn't
follow any particular format, you can also set this to the current date or
`banana` or anything you want really.

Or to build a statically linked binary:

    $ go build -ldflags="-X main.version=$(git log -n1 --format='%h_%cI')" \
        -tags osusergo,netgo,sqlite_omit_load_extension \
        -ldflags='-extldflags=-static' \
        ./cmd/goatcounter

You'll now have a `goatcounter` binary in the current directory.

You need Go 1.16 or newer and a C compiler (for SQLite), or compile it with
`CGO_ENABLED=0 go build` and use PostgreSQL.

It's recommended to use the latest release as in the above command. The master
branch should be reasonably stable but no guarantees, and sometimes I don't
write detailed release/upgrade notes until the actual release.

It's not recommended to use `go get` in GOPATH mode since that will ignore the
dependency versions in go.mod.

### Running

You can start a server with:

    $ goatcounter serve

The default is to use a SQLite database at `./db/goatcounter.sqlite3`, which
will be created if it doesn't exist yet. See the `-db` flag and
`goatcounter help db` to customize this.

GoatCounter will listen on port `*:80` and `*:443` by default. You don't need
to run it as root and can grant the appropriate permissions on Linux with:

    $ setcap 'cap_net_bind_service=+ep' goatcounter

Listening on a different port can be a bit tricky due to the ACME/Let's Encrypt
certificate generation; `goatcounter help listen` documents this in depth.

You can create new sites with the `create` command:

    $ goatcounter create -email me@example.com -domain stats.example.com

This will ask for a password for your new account; you can also add a password
on the commandline with `-password`. If you use a custom DB, you must also pass
the `-db` flag here.

### Updating

You may need to run the database migrations when updating. Use  `goatcounter
-automigrate` to always run all pending migrations on startup. This is the
easiest way, although arguably not the "best" way.

Use `goatcounter migrate <file>` or `goatcounter migrate all` to manually run
migrations; generally you want to upload the new version, run migrations while
the old one is still running, and then restart so the new version takes effect.

Use `goatcounter migrate show` to get a list of pending migrations.

### PostgreSQL

Both SQLite and PostgreSQL are supported. SQLite should work well for most
smaller sites, but PostgreSQL gives some better performance:

1. Run with custom `-db` flag:

       $ goatcounter serve -db 'postgresql://dbname=goatcounter'

   The following example will connect to postgresql via unix socket:

       $ goatcounter serve \
           -db 'postgresql://host=/run/postgresql dbname=goatcounter sslmode=disable'


   See the [pq docs][pq] for more details on the connection string.

2. You can compile goatcounter without cgo if you don't use SQLite:

       $ CGO_ENABLED=0 go build -ldflags="-X main.version=$(git log -n1 --format='%h_%cI')" ./cmd/goatcounter

   Functionally it doesn't matter too much, but builds will be a bit easier and
   faster as it won't require a C compiler.

[pq]: https://pkg.go.dev/github.com/lib/pq#hdr-Connection_String_Parameters

### Development/testing

You can start a test/development server with:

    $ goatcounter serve -dev

The `-dev` flag makes some small things a bit more convenient for development;
TLS is disabled by default, it will listen on localhost:8081, the application
will automatically restart on recompiles, and a few other minor changes.

See [.github/CONTRIBUTING.markdown](/.github/CONTRIBUTING.markdown) for more
details on how to run a development server, write patches, etc.

Various aggregate data files are available at https://www.goatcounter.com/data
