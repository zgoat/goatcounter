-- Find User-Agents that weren't fully parsed by gadget.
with x as (
	select user_agent_id, ua,
		browsers.name || ' ' || browsers.version as browser,
		systems.name || ' ' || systems.version as system
	from user_agents
	join browsers using(browser_id)
	join systems using(system_id)
	where isbot in (0, 1) and (browsers.name='' or browsers.version='' or systems.name='' or
		(systems.name not in ('Linux', 'Chrome OS', 'FreeBSD') and systems.version=''))
)
select
	user_agent_id as id,
	count(user_agent_id) as count,
	min(browser) as browser,
	min(system) as system,
	min(x.ua)
from x
join hits using (user_agent_id)
group by user_agent_id
order by count desc;
