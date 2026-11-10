create table users (
    id           uuid primary key,
    discord_id   text not null unique,
    username     text not null,
    display_name text not null,
    avatar_url   text not null default '',
    created_at   timestamptz not null default now()
);

create table sessions (
    token_hash bytea primary key,
    user_id    uuid not null references users on delete cascade,
    expires_at timestamptz not null
);

create table login_codes (
    code_hash  bytea primary key,
    user_id    uuid not null references users on delete cascade,
    challenge  text not null,
    expires_at timestamptz not null
);

create table guilds (
    id         uuid primary key,
    name       text not null,
    icon_url   text not null default '',
    owner_id   uuid not null references users,
    created_at timestamptz not null default now()
);

-- The role whose id equals guild_id is @everyone.
create table roles (
    id          uuid primary key,
    guild_id    uuid not null references guilds on delete cascade,
    name        text not null,
    color       int not null default 0,
    permissions bigint not null,
    position    int not null default 0
);
create index on roles (guild_id);

create table members (
    guild_id  uuid not null references guilds on delete cascade,
    user_id   uuid not null references users on delete cascade,
    nickname  text not null default '',
    joined_at timestamptz not null default now(),
    primary key (guild_id, user_id)
);
create index on members (user_id);

create table member_roles (
    guild_id uuid not null,
    user_id  uuid not null,
    role_id  uuid not null references roles on delete cascade,
    primary key (guild_id, user_id, role_id),
    foreign key (guild_id, user_id) references members on delete cascade
);

create table bans (
    guild_id   uuid not null references guilds on delete cascade,
    user_id    uuid not null references users on delete cascade,
    created_at timestamptz not null default now(),
    primary key (guild_id, user_id)
);

create table channels (
    id         uuid primary key,
    guild_id   uuid not null references guilds on delete cascade,
    type       smallint not null,
    name       text not null,
    topic      text not null default '',
    position   int not null default 0
);
create index on channels (guild_id);

create table channel_overwrites (
    channel_id uuid not null references channels on delete cascade,
    target_id  uuid not null,
    is_member  boolean not null,
    allow      bigint not null,
    deny       bigint not null,
    primary key (channel_id, target_id)
);

-- Message ids are UUIDv7, so ordering by id is ordering by time.
create table messages (
    id         uuid primary key,
    channel_id uuid not null references channels on delete cascade,
    author_id  uuid not null references users,
    content    text not null,
    created_at timestamptz not null default now(),
    edited_at  timestamptz
);
create index on messages (channel_id, id desc);

create table invites (
    code       text primary key,
    guild_id   uuid not null references guilds on delete cascade,
    creator_id uuid not null references users,
    max_uses   int not null default 0,
    uses       int not null default 0,
    expires_at timestamptz
);
