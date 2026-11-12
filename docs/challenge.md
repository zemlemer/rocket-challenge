# The Challenge

In this challenge, you will build one or more services that consume messages from a set of
rockets and expose their state through a REST API. We imagine this API being used by
something like a dashboard, but you are not expected to build the dashboard itself.

At a minimum, we expect endpoints that can:

1. Return the current state of a given rocket (type, speed, mission, etc.)
2. Return a list of all the rockets in the system; preferably with some kind of sorting.

The service should also expose an endpoint that the test program can post messages to.

We write our services in [Go](https://go.dev/), but there are no constraints on which language
you choose for the challenge. We would rather see a great solution in a language you are
comfortable with than a mediocre solution written in Go.

## The messages

Each rocket will be dispatching various messages (encoded as JSON) about its state changes
through individual radio channels. The channel is unique for each rocket and can therefore be
treated as the ID of the rocket.

In addition to the channel, each message contains a message number, which represents the
message's order within a channel; a message time, which indicates when the message was sent;
and a message type, which describes the event that occurred.

**Important:** Messages will arrive out of order, and delivery follows an at-least-once
guarantee, which means you may receive the same message more than once. Messages are
redelivered if not successfully delivered, e.g. when receiving a non-2xx HTTP status code.

Example `RocketLaunched` message:

```json
{
    "metadata": {
        "channel": "193270a9-c9cf-404a-8f83-838e71d9ae67",
        "messageNumber": 1,
        "messageTime": "2022-02-02T19:39:05.86337+01:00",
        "messageType": "RocketLaunched"
    },
    "message": {
        "type": "Falcon-9",
        "launchSpeed": 500,
        "mission": "ARTEMIS"
    }
}
```

### Message types

**RocketLaunched** — sent out once: when a rocket is launched for the first time.

```json
{ "type": "Falcon-9", "launchSpeed": 500, "mission": "ARTEMIS" }
```

**RocketSpeedIncreased** — continuously sent out: when the speed of a rocket is increased by a certain amount.

```json
{ "by": 3000 }
```

**RocketSpeedDecreased** — continuously sent out: when the speed of a rocket is decreased by a certain amount.

```json
{ "by": 2500 }
```

**RocketExploded** — sent out once: if a rocket explodes due to an accident/malfunction.

```json
{ "reason": "PRESSURE_VESSEL_FAILURE" }
```

**RocketMissionChanged** — continuously sent out: when the mission for a rocket is changed.

```json
{ "newMission": "SHUTTLE_MIR" }
```

## Running the test program

In the ZIP file, locate the executable that matches your system and run:

```sh
./rockets launch "http://localhost:8088/messages" --message-delay=500ms --concurrency-level=1
```

This launches the program, which starts posting (request method: POST) messages to the URL
provided with a delay of 500ms between each message.

To see all commands run `./rockets help` and for help on the launch command run
`./rockets launch --help`.

We are going to run the program against your solution with the default values.
