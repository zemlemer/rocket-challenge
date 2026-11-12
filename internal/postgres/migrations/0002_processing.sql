-- Processing status of inbox entries. Entries are never deleted: the
-- processor marks them, so the inbox stays the full record and keeps
-- answering redeliveries through its unique identity index.
alter table inbox
  add column processed_at timestamptz null,
  add column outcome      text null check (outcome in ('applied', 'quarantined', 'failed')),
  add column reason       text null,
  add constraint inbox_processed_has_outcome check ((processed_at is null) = (outcome is null));

-- Unprocessed entries in arrival order, independent of processed history.
create index inbox_pending_idx on inbox (id) where processed_at is null;

-- Current state per rocket, written only by order-independent updates.
create table rockets (
  channel                     text        primary key,
  speed                       bigint      not null default 0,  -- launch speed + increments - decreases
  rocket_type                 text        null,                -- null = not launched (message 1 not processed yet)
  launch_speed                bigint      null,
  launched_at                 timestamptz null,
  mission                     text        null,
  mission_message_number      bigint      not null default 0,  -- number of the message the mission came from
  explosion_reason            text        null,                -- not null = exploded
  last_applied_message_number bigint      not null default 0,
  last_applied_message_at     timestamptz null,
  updated_at                  timestamptz not null default now()
);
