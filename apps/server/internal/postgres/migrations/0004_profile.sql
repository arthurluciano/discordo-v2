-- The rest of a profile, shown on the profile card. banner_color is 0xRRGGBB, or 0 for the theme's own banner.
alter table users add column bio text not null default '';
alter table users add column pronouns text not null default '';
alter table users add column banner_color integer not null default 0;
