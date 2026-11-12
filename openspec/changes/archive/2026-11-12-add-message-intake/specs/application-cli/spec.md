# Spec Delta

## ADDED Requirements

### Requirement: Database connection pool
Modes that use the database SHALL keep a pool of reusable connections with a bounded size (default 10 connections per process) and a bounded time to establish a connection (default 5 s). Both SHALL be overridable through the database URL parameters `pool_max_conns` and `connect_timeout`.

#### Scenario: Defaults
- **WHEN** a mode connects with a database URL without pool parameters
- **THEN** the pool holds at most 10 connections and a connection attempt is abandoned after 5 s

#### Scenario: Override through the URL
- **WHEN** the database URL contains `pool_max_conns=3&connect_timeout=2`
- **THEN** the pool holds at most 3 connections and a connection attempt is abandoned after 2 s

#### Scenario: Unresponsive database host
- **WHEN** the database host accepts no TCP connection and drops packets silently
- **THEN** a request needing the database fails within the connect timeout instead of hanging
