## MODIFIED Requirements

### Requirement: No size limit at the entry point
The entry point SHALL forward request bodies of any size to the server instances, so that size handling is decided only by intake (see Oversized bodies) and never answered with `413` by the load balancer.

#### Scenario: 2 MiB body through the load balancer
- **WHEN** a client delivers a complete 2 MiB body to `localhost:8088/messages`
- **THEN** the response is `202` after the inbox stores an entry with `truncated = true`, zero body bytes and no parsed metadata
