ChangeLog for GoatCounter
=========================

This list is not comprehensive, and only lists new features and major changes,
but not every minor bugfix. The goatcounter.com service generally runs the
latest master.

Unreleased v2.0.0
-----------------

The version is bumped because this contains a number of incompatible changes;
several `goatcouner` commands got changed. It also includes some potentially
database migrations which change quite a few things, and running them is a bit
more complex than the standard migrations.

An overview of **incompatible** changes:

- `goatcounter migrate` is now `goatcounter db migrate`. It also behaves a bit
  different:

  - `goatcounter migrate pending` lists only pending migrations, and will use
    exit code 1 if there are any pending migrations.
  - `goatcounter migrate list` lists all migrations, always exits with 0.

- `goatcounter create` is now `goatcounter db site create`, and some flags got
  changed:

  - `-domain` is now `-vhost`.

- The `-site` flag got removed from `goatcounter import`; you can now only use
  `-url` to set a GoatCounter site to import to. You can use `goatcounter db
  apikey create` to create an API key from the commandline.

- The build flag to set the version changed from:

      -ldflags="-X main.version=..."

  to:

      -ldflags="-X zgo.at/goatcounter.Version=..."

- There are some rather large changes to the database layout (#383) for better
  efficiency (**see below, you really want to read this**).

Other changes:

- You can import pageviews from logfiles with the `goatcounter import` command;
  see `goatcounter help import` for details.

- New `goatcounter buffer` command; this allows buffering of pageviews in case
  the backend is down, running migrations, etc. See `goatcounter help buffer`
  for more information.

- You can now configure what's displayed on the dashboard, in what order, and
  configure some aspects of various "widgets". This is in the *Setting →
  Dashboard*, and some settings from the main settings page have moved there.
  (#416, #417, #418)

- You can save a default view for the dashboard. Instead of always loading the
  last week by default, you can now configure it to load the last month, or view
  by day, or anything you want really. (#419)

- You can choose which data to collect; you can disable collecting any
  User-Agent, location, or Referrer information. (#423)

- Ability to record state/province/district in addition to country, so it
  records "US-TX" or "NL-NB" instead of "United States" or "Netherlands". This
  option can be disabled separately from recording the country (enabled by
  default). This requires specifying the path to a GeoIP City database, which
  isn't included since it's ~30M (#425)

- There are now stable `count.v*.js` scripts that can use subresource integrity.
  See the integration code for a list and hashes.

- You can use `data-goatcounter-settings` on the `<script>` tag to load the
  settings (this requires `count.v2.js` or newer).

- The database for PostgreSQL is now created automatically; you no longer need
  to do this manually.

- You can copy settings from a site to other sites in *Settings → Sites*.

- Add a `gcbench` utility for inserting random pageviews in a database; for
  testing and comparing performance. This might be useful for end-users too in
  some cases, for example to see how much performance difference SQLite and
  PostgreSQL will give you, or to test if frobbing with server settings makes a
  difference:

      $ go run ./cmd/gcbench -db sqlite://db/gcbench.sqlite3 -ndays=90 -npaths=100 -nhits=1_000_000
      $ go run ./cmd/gcbench -db postgresql://dbname=gcbench -ndays=90 -npaths=100 -nhits=1_000_000

  Right now it doesn't try super-hard to simulate read-world usage patterns: the
  distribution is always uniform, but it still gives a reasonably accurate
  indication for comparison purposes.

- Many other minor changes and improvements.

---

This release contains some rather large changes to the database layout (#383);
this means:

- Somewhat faster queries.
- Greatly reduced disk space requirements for the database.
- The Browsers, systems, size, and location stats are filtered if you enter
  something in "filter paths". Previously this always displayed the site totals.
- "Purge path" now works as expected for all stats (fixes #96).
- Easier to add new statistics in the future.

1. You **must** first update to 1.4.2 and run all migrations from that.
   **Updating from older versions directly to 1.5.0 will not work!**

2. Run the migrations with `goatcounter serve -automigrate` or `goatcounter
   migrate`.

3. You probably want to manually run `VACUUM` (or `VACUUM FULL` for PostgreSQL)
   after the migration to free up unused rows. This isn't required though; it
   just frees up disk space.

4. Run `goatcounter reindex`.

All of this may take a while if you've got a lot of data. For about 500,000
pageviews it takes about 3 minutes on SQLite, but if you've got millions of
pageviews it may take an hour or more.

**Note**: the CSV export format was increased to `2`; it now includes the parsed
browser and system values in addition to the User-Agent header. Version 1.5 will
not be able to import the older exports from version `1`.


2020-11-10, v1.4.2
------------------

- Add a "visitor counter" image you can add to your website to display the
  number of visitors, similar to old-style counters back in the ’90s (#398).

- Other than this, it's mostly contains a few minor bugfixes and the like. You
  can see a list of changes in the git log:
  https://github.com/zgoat/goatcounter/compare/v1.4.1...v1.4.2


2020-09-04 v1.4.1
-----------------

A few small updates, fixes, and performance enhancements. Nothing major.

You can see a list of changes in the git log:
https://github.com/zgoat/goatcounter/compare/v1.4.0...v1.4.1


2020-08-24 v1.4.0
-----------------

- **Change defaults for `-listen`** (#336)

  The default for the `-listen` flag changed from `localhost:8081` to `:443`,
  which is probably a better and less confusing default for most people. There
  is also some more detailed docs available in `goatcounter help listen`.

- Set Cache-Control header for static files (#348)

  The `Cache-Control` header is now set for static files. Since the "cache
  busting" happens based on the goatcounter version it's now recommended to set
  this if you're compiling GoatCounter yourself. See the updated README for
  instructions.

- Add multi-factor auth (#306)

  TOTP-based multi-factor auth is now supported.

- Better export, export API, add import feature (#316, #318, #329)

  You can now import the CSV exports, useful for migrating from self-hosted to
  goatcounter.com or vice versa, or for migrating from other systems. There is a
  web interface and a `goatcounter import` command.

  The export now supports a "pagination cursor", so you can export only rows you
  didn't previously export. This is especially useful with the new export API.
  which should make it easy to sync GoatCounter data with another external
  platform.

  See http://goatcounter.com/api for details on the export API.

- API for sending pageviews (#357)

  Doing that with the regular `/count` is actually quite painful, as you quickly
  run in to ratelimits, need to set specific headers, etc. Adding an API
  endpoint for that makes things much easier.

- API for creating and editing additional sites (#361)

- Some redesigns (#324, #315, #321 #320)

  The "Totals" is now placed below the Pages; I think it makes more sense there.
  The Y-axis for the totals is now also independent. There's also been a quite a
  few restylings.

- Add "text view" mode (#359)

  View your data as a simple table without too much graphics; only the main
  "Pages" overview is implemented for now.

- Make it easier to skip your own views (#290)

  Previously this required adding custom code, but now loading any page with
  `#toggle-goatcounter` added will enable/disable the GoatCounter tracking for
  that browser.

- Can now manage "additional sites" from self-hosted GoatCounter (#363)

  This wasn't possible before for no other reason than laziness on my part 🙃

- public/count.js is now ISC licensed (#309)

  Previously the EUPL applied, which is fairly restrictive and may prevent
  people from including/self-hosting the count.js script.

- Add `goatcounter db` command

  This is mostly useful for writing deploy scripts: `goatcounter db
  schema-sqlite` prints the SQLite schema, `schema-pgsql` prints the PostgreSQL
  schema, and `goatcounter db test` tests if the database exists.

- Session hashes are no longer persisted to the database

  This is kind of an internal change, but session hashes are now stored in
  memory only and never recorded to the database. There's no real reason to
  persistently store this information, and this is a (small) privacy/GDPR
  compliance improvement.


2020-06-01 v1.3.0
-----------------

Note: this release contains quite a few database migrations; they make take a
minute to run (depending on your table size), and you may want to run a `VACUUM`
afterwards.

- Remove email auth, replace `-auth` with `-email-from` (#263, #270)

  As mentioned in the 1.2 release the email authentication is now removed. You
  can still reset the password for old accounts.

  Since the email auth no longer exists the `-auth` parameter no longer makes
  sense. It's now replaced with `-email-from`, which can be set to just an email
  address.

  **Action required**: if you set the email address with `-auth` you'll have to
  change it to `-email-from`.

- Add OS stats, improve accuracy of browser stats (#261)

  GoatCounter now tracks the OS/platform in addition to just the browser, and
  the accuracy of the browser stats should be improved.

  **Action required**: you'll need to populate the `system_stats` table:

      $ goatcounter reindex -table system_stats

  If you want to process all browser stats with the new logic too, then use this
  instead:

      $ goatcounter reindex -table system_stats,browser_stats

- Improve performance (#265, #273, #274)

  Increase performance by quite a bit on large sites and time ranges.

- Remove the per-path scaling (#267)

  Previously GoatCounter would scale the Y-axis different for every path in the
  dashboard, but this was more confusing than helpful. It's now always scaled to
  the maximum of all paths in the selected date range and filter, with a field
  to scale it lower on-demand if desired.

- Add totals overview (#271)

  Add chart with totals for the selected date range and filter.

- Add `goatcounter.url()`, `goatcounter.filter()` (#272, #253)

  Adds two new methods to the `count.js` script so it's easier to use write own
  implementation. In addition the script will now issue a `console.warn()` if a
  request isn't being counted for some reason.


2020-05-18 v1.2.0
-----------------

There are a number of changes in 1.2, and a few which require a bit of action
when updating. Also see: https://www.arp242.net/goatcounter-1.2.html

- Password authentication (#232)

  The email-based authentication has been deprecated in favour of password
  authentication.

  **Action required** Use the interface to set a password (you will get a
  notification about this). Email authentication still works, but will be
  removed in the next release, after which updating the password will be tricky.

- Unique visit tracking (#212)

  GoatCounter now tracks unique visits (without using cookies).

  Technical documentation about the implementation is in
  [doc/sessions.markdown](doc/sessions.markdown).

  There are two ways to display the older stats:

  1. Do nothing; meaning that "visits" will be 0 for previous date ranges.

  2. Assign a new 'session' to every hit, so that unique visits will be the same
     as the number of pageviews.

  Doing option 2 is a potentially expensive database operation and not everyone
  may care so it's not done automatically; instructions for doing this are:

  - SQLite (do *not* do this on a running system; as far as I can tell there's
    no good way to get the next sequence ID while incrementing it):

        delete from sessions;
        update hits set session=id, first_visit=1;
        update sqlite_sequence set seq = (select max(session) from hits) where name='sessions';

  - PostgreSQL:

        update hits set session=nextval('sessions_id_seq'), first_visit=1;

  And then run `goatcounter reindex`.

- Improve bot detection (#219)

  The bot detection is now improved; this will be applied to older pageviews in
  the database with a migration, but the cached statistics aren't updated
  automatically (as it can take a while for larger sites). Use the `reindex`
  command to fully update older pageviews (this is entirely optional).

- Track events (#215)

  There is now better support to track events; see the updated documentation on
  the Site Code page for details.

- Better support for campaigns (#238)

  There is now a "campaign parameters" setting; if the URL matches one of these
  parameters it will be set as the referrer (overriding the `Referer` header).

  The default is `utm_campaign, utm_source, ref`.

- Better export (#221)

  The export was a quick feature added in the first version, but didn't scale
  well to larger sites with a lot of pageviews. This now works well for any
  number of pageviews.

- Many small improvements and bug fixes

  It's almost 2 months of work, and there have been many small changes, fixes,
  and improvements. I didn’t keep track of them all 😅


2020-03-27 v1.1.2
-----------------

- Fix small issue with the domain not showing correct in the site code 😅


2020-03-27 v1.1.1
-----------------

- Small bugfix release which fixes some small issues and improves a few small
  documentation issues. List of changes:
  https://github.com/zgoat/goatcounter/compare/v1.1.0...v1.1.1

- The biggest change is that the `saas` command no longer works (and is no
  longer documented). It was only ever useful for hosting goatcounter.com, and
  has a number of assumptions and hard-coded values.

  If you're using `saas`, then you can migrate to `serve` by setting a custom
  domain (`sites.cname`) for all the sites. The `serve` command should work
  after that.


2020-03-18 v1.1.0
-----------------

- **Incompatible** Improve CLI UX (#154, #173, #175, #181)

  The entire CLI has been redone; the original wasn't very user-friendly for
  self-hosting. See `goatcounter help` for the full docs, but in brief:

      o Use "goatcounter serve" instead of just "goatcounter".
      o Create new sites with "goatcounter create".
      o Good support for TLS hosting and ACME certificates (see -tls flag).
      o Invert -prod to -dev (i.e. just drop -prod for production services, add -dev for development).
      o -smtp flag is no longer required.
      o -dbconnect                 →  -db
      o -pgsql                     →  -db postgresql://...
      o -staticdomain              →  no longer needed, but if you really want it you can
                                      append to domain: -domain example.com,static.example.com
      o -emailerrors               →  -errors mailto:...
      o goatcounter -migrate       →  goatcounter migrate
      o goatcounter -migrate auto  →  goatcounter serve -automigrate

- **Action required** Show top referrals (#192)

  To populate the ref_stats and size_stats tables for older data, update first
  and then run:

      $ goatcounter reindex -confirm -table ref_stats
      $ goatcounter reindex -confirm -table size_stats

- Charts are displayed in local timezone (#155)

- Add "IgnoreIPs" setting to ignore your own views (#128)

- Link to paths by adding a new domain setting (#138)

- Add configurable data retention (#134)

- Allow configuring the thousands separator (#132)

- Allow filtering pages in the dashboard (#106)

- Improve the integration code (#122)

- Allow sending emails without a relay (#184)

- Add get_query() to count.js to get query parameter (#199)

- Allow viewing the charts by day, instead of only by hour (#169)


2020-01-13 v1.0.0
-----------------

Initial release.
