-- Get an aggregate of all User-Agents that are not bots.
with x as (
	select
		user_agent_id, ua,
		browsers.name    as browser_name,
		browsers.version as browser_version,
		systems.name     as system_name,
		systems.version  as system_version
	from user_agents
	join browsers using (browser_id)
	join systems  using (system_id)
	where isbot in (0, 1)
)
select
	count(user_agent_id)  as count,
	date(min(created_at)) as first_seen,
	browser_name,
	browser_version,
	system_name,
	system_version,
	ua
from hits
join x using (user_agent_id)
where
	created_at > '2019-07-29' and
	hits.bot = 0 and
	first_visit=1
group by user_agent_id, ua, browser_name, browser_version, system_name, system_version;
