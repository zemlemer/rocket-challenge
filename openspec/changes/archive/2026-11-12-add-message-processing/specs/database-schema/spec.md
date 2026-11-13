# Spec Delta

## ADDED Requirements

### Requirement: Inbox processing status
Every inbox entry SHALL carry its processing status: when it was processed (nothing while unprocessed), its outcome (`applied`, `quarantined` or `failed`) and, for quarantined and failed entries, the reason. Unprocessed entries SHALL be findable in arrival order without scanning processed ones.

#### Scenario: Entry marked
- **WHEN** an entry is processed
- **THEN** its processing time and outcome can be read from the inbox, and its body and metadata are unchanged

### Requirement: Rocket state storage
The schema SHALL store one row per rocket holding everything rocket-state defines: speed, launch attributes (none until launched), mission and `mission_message_number`, explosion reason (none until exploded), and `last_applied_message_number` with `last_applied_message_at`.

#### Scenario: State survives a restart
- **WHEN** messages are processed and all processes are restarted
- **THEN** every rocket's state is unchanged
