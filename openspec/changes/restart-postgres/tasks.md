# Tasks

## 1. PostgreSQL restart policy

- [x] 1.1 Add `restart: unless-stopped` to the `postgres` service in `compose.yaml`; verify with `docker compose config`, `docker inspect` showing the policy, and a PostgreSQL process stopped with `pg_ctl stop -m immediate` whose container restarts, becomes healthy, and lets `GET /stats` through `localhost:8088` answer `200` without a manual command.
