# Proposal

## Why

Rocket state is stored after message processing, but clients cannot read it through HTTP. The challenge requires both a single-rocket view and a list of rockets, preferably sortable.

## What Changes

- Add `GET /rockets/{channel}` for the current processed state of a rocket that is launched or exploded. Return `404` until its launch or explosion has been processed, and for a channel that does not satisfy the channel rule.
- Add `GET /rockets` for all launched or exploded rockets as `{"rockets": [...]}`. An exploded rocket whose launch is not processed yet is shown with `null` launch fields, which sort last. One sort field (`channel`, `type`, `mission`, `speed`, `status` or `launchedAt`, default `channel`) and direction (`asc` or `desc`, default `asc`) are chosen with `?sort=` and `?order=`; ascending channel breaks ties. An invalid sort field or direction gets `400`.
- Return the same rocket representation from both endpoints. Derive status as `active` or `exploded` from the stored explosion reason.
- Hide rocket rows whose channel does not satisfy the channel rule of message validation. Such rows may remain from before that rule existed; the restrict-rocket-channels change left their exclusion to this API.
- Reads reflect committed processing results; a successful message intake response does not guarantee the new state is visible immediately. Reads keep working while processors are stopped.
- Answer `503` when the database is unavailable or does not answer within 5 s, as message intake does for unavailability.
- Make edge cases explicit: case-sensitive channels and parameter values, `HEAD` allowed, `400` for a malformed query string, cleaned paths redirected, and message times outside years 0000–9999 that still produce a valid response instead of `500`.
- Return the full list without pagination. If the rocket count grows enough to make this impractical, add pagination in a separate change.

## Capabilities

### New Capabilities

- `rocket-query`: reading the state of launched or exploded rockets through HTTP, including visibility, representation, sorting, errors and read consistency.

### Modified Capabilities

None. The existing http-server rules for unknown routes and wrong methods apply to the new routes unchanged.

## Impact

- Adds two read routes to the existing server and read-only queries against the current `rockets` table.
- Updates `README.md` (endpoints, sorting, consistency, limitations).
- Does not change message intake, processing, persistence schema, or deployment topology.
