-- Every guild has one world (channel type 4): the map where members walk and talk. New guilds are born with it.
insert into channels (id, guild_id, type, name, position)
select gen_random_uuid(), g.id, 4, 'Mundo', 0
from guilds g
where not exists (select 1 from channels c where c.guild_id = g.id and c.type = 4);
