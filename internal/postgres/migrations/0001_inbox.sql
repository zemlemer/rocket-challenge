-- Inbox: every accepted request body, stored unchanged, with the metadata
-- parsed from it. A metadata column is null when that field was malformed.
create table inbox (
  id             bigserial   primary key,
  received_at    timestamptz not null default now(),
  channel        text        null,      -- metadata.channel; with message_number the message identity
  message_number bigint      null,      -- metadata.messageNumber
  message_time   timestamptz null,      -- metadata.messageTime
  message_type   text        null,      -- metadata.messageType, as sent (not checked against known types)
  body           bytea       not null,  -- raw: may be invalid JSON or truncated
  truncated      boolean     not null,
  check ((channel is null) = (message_number is null))
);

-- One entry per identity; entries without identity are not constrained.
create unique index inbox_identity_uq on inbox (channel, message_number)
  where channel is not null;
