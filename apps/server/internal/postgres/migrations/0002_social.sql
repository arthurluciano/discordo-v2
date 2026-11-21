-- DMs are channels without a guild (type 2 = 1:1, 3 = group).
alter table channels alter column guild_id drop not null;
alter table channels add column dm_key text unique; -- "<lower user id>:<higher user id>": one 1:1 channel per pair

create table dm_members (
    channel_id uuid not null references channels on delete cascade,
    user_id    uuid not null references users on delete cascade,
    primary key (channel_id, user_id)
);
create index on dm_members (user_id);

-- One row per direction. status: 1 friend, 2 pending incoming, 3 pending outgoing, 4 blocked.
create table relationships (
    user_id   uuid not null references users on delete cascade,
    target_id uuid not null references users on delete cascade,
    status    smallint not null,
    primary key (user_id, target_id)
);

alter table messages add column mentions uuid[] not null default '{}';
alter table messages add column mention_everyone boolean not null default false;

-- message_id is null between upload and the message that carries the file.
create table attachments (
    id           uuid primary key,
    channel_id   uuid not null references channels on delete cascade,
    uploader_id  uuid not null references users,
    message_id   uuid references messages on delete cascade,
    filename     text not null,
    content_type text not null,
    size         bigint not null,
    created_at   timestamptz not null default now()
);
create index on attachments (message_id);

create table reactions (
    message_id uuid not null references messages on delete cascade,
    user_id    uuid not null references users on delete cascade,
    emoji      text not null,
    primary key (message_id, user_id, emoji)
);
