-- Get an aggregate of all screen sizes.
select
	date(min(created_at)) as first_seen,
	count(*) as count,
	replace(size, ' ', '') as size
from hits
where
	bot=0 and
	size != '' and
	first_visit=1
group by size;
