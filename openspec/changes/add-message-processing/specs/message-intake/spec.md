# Spec Delta

## MODIFIED Requirements

### Requirement: Duplicate deliveries at intake
A request whose identity (`channel`, `messageNumber`) is already in the inbox — waiting or already processed — SHALL be answered with `202` without storing a second copy; the first stored body wins. Inbox entries are never deleted, so the inbox alone decides the answer to a sender: the response SHALL NOT depend on whether or how the message was processed.

#### Scenario: Redelivery while waiting
- **WHEN** the same message is delivered three times before it is processed
- **THEN** every delivery is answered with `202` and the inbox holds one copy

#### Scenario: Redelivery after processing
- **WHEN** a message is processed and then delivered again
- **THEN** the redelivery is answered with `202`, nothing new is stored, and rocket state is unchanged

#### Scenario: Redelivery of a quarantined message
- **WHEN** a message was processed with outcome `quarantined` and is delivered again
- **THEN** the redelivery is answered with `202` and the inbox still holds the single quarantined entry
