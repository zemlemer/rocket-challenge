# Spec Delta

## MODIFIED Requirements

### Requirement: Read failures are not retried
The entry point SHALL NOT retry a `503` response to a read request on `/rockets` paths or `/stats` on another instance, because a slow or unavailable database affects every instance alike. It SHALL still retry such a request on another instance when the connection to an instance fails.

#### Scenario: Database paused during a read
- **WHEN** the database is paused and a client sends `GET /rockets` through `localhost:8088`
- **THEN** the client gets `503` after one read timeout (about 5 s), not after one timeout per instance

#### Scenario: Database paused during a stats read
- **WHEN** the database is paused and a client sends `GET /stats` through `localhost:8088`
- **THEN** the client gets `503` after one read timeout (about 5 s), not after one timeout per instance

#### Scenario: Intake right after a failed read
- **WHEN** the database answers again right after that read
- **THEN** `POST /messages` requests through `localhost:8088` get `202` on every instance
