alter table users drop column role;
alter table users add column access jsonb not null default '{"0":"a"}';

-- {
-- 	1: "rw",
-- 	2: "rw"
-- 	0: "ro" // new sites
-- }

-- // Only rw to site 1, 2. not added for new sites.
-- {1: "rw", 2: "rw"}

-- // Admin user; don't need to store much more.
-- {0: "a"}
