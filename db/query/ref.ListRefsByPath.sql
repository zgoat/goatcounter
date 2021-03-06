with x as (
	select path_id from paths
	where site_id = :site and lower(path) = lower(:path)
)
select
	coalesce(sum(total), 0)        as count,
	coalesce(sum(total_unique), 0) as count_unique,
	max(ref_scheme)                as ref_scheme,
	ref                            as name
from ref_counts
join x using (path_id)
where
	site_id = :site and hour >= :start and hour <= :end
group by ref
order by count_unique desc, ref desc
limit :limit offset :offset
