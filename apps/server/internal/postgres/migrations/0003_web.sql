-- The web client logs in with a session cookie set straight from the OAuth callback.
drop table login_codes;

-- Set once the user picks their own name or avatar, so logging in again keeps them.
alter table users add column custom_name boolean not null default false;
alter table users add column custom_avatar boolean not null default false;

alter table guilds add column banner_url text not null default '';
