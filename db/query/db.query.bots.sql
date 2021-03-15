-- Get an aggregate of all bot User-Agents.
with x as (
	select user_agent_id, ua from user_agents
	where isbot not in (0, 1)
)
select
	count(user_agent_id)  as count,
	date(min(created_at)) as first_seen,
	ua
from hits
join x using (user_agent_id)
where
	created_at > '2019-07-29' and
	hits.bot in (3, 4, 5, 6, 7, 150, 151, 152, 153) and
	first_visit=1
group by user_agent_id, ua;
