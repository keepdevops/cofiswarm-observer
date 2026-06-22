# cofiswarm-observer

Telemetry plugins + agent log sinks. FHS: `/var/lib/cofiswarm/observer/plugins`, `/var/log/cofiswarm/agent_logs`.

Default: `:8016`

## Bus ingest (`/v1/observed`)

The live roster/alerts view tails the bus. Pick the read path by env:

| Env | Effect |
|-----|--------|
| `COFISWARM_ZMQ_EGRESS_ADDR=tcp://127.0.0.1:5557` | Subscribe to the zmq-bridge ZMQ egress PUB wire (filter `COFISWARM_ZMQ_FILTER`, default `swarm.`). |
| `COFISWARM_BRIDGE_URL=http://127.0.0.1:5555` | Tail the zmq-bridge SSE stream (`/v1/stream`). |

ZMQ ingest takes precedence when both are set. `COFISWARM_BRIDGE_URL` also carries
presence/hello republished over `/v1/publish`; with only the ZMQ address set, ingest runs
read-only (republish is skipped, logged).
