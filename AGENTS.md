# Rockets Backend Challenge

## Goal
Build a correct, maintainable backend within 6 hours.

## Development rules
- Use OpenSpec as the source of truth for requirements.
- Do not implement features before the spec is reviewed.
- Prefer simple architecture over unnecessary abstractions.
- Implement one logical task at a time.
- Keep HTTP, domain logic, and persistence separated folowing Clean Architecture guidelines.
- Run relevant tests after each implementation step.
- Explain architectural changes before making them.
- Do not silently modify approved requirements.

## Correctness priorities
1. Per-rocket message ordering
2. Idempotent message processing
3. Concurrent request safety
4. Reliable state transitions
5. Clear error handling

## Verification
- Test duplicates and out-of-order delivery.
- Test concurrent requests.
- Test recovery if persistence is used.
- Run the provided Rockets simulator.
- Report any remaining limitations.

## AI accountability
- Distinguish verified behavior from assumptions.
- Record meaningful design trade-offs.
