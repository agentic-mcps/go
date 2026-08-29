# v0.8.0 reproducible evaluation

This directory defines eight pinned historical tasks and the neutral result
contracts used to evaluate agentic-go. It contains no third-party source and no
paid-model result.

Build the local runner with the repository's supported Go toolchain:

```sh
go build -o /tmp/agentic-go-eval ./validation/cmd/eval
```

Validate the complete corpus:

```sh
/tmp/agentic-go-eval validate \
  -tasks validation/v0.8.0/tasks
```

Prepare private, content-addressed fixture bundles from the user-cloned
repositories:

```sh
/tmp/agentic-go-eval prepare \
  -tasks validation/v0.8.0/tasks \
  -sources /path/to/agentic-go-validation \
  -output /path/to/private/evaluation/v0.8.0/bundles
```

The output path above is illustrative. Bundles and response artifacts belong
outside this repository. Preparation never changes the source clones.

Set up and score one candidate:

```sh
/tmp/agentic-go-eval setup \
  -task validation/v0.8.0/tasks/grpc-go-clone-token/task.json \
  -bundle /path/to/grpc-go-clone-token-<sha256>.bundle \
  -workspace /tmp/grpc-go-clone-token

/tmp/agentic-go-eval score \
  -task validation/v0.8.0/tasks/grpc-go-clone-token/task.json \
  -bundle /path/to/grpc-go-clone-token-<sha256>.bundle \
  -workspace /tmp/grpc-go-clone-token \
  -output /tmp/grpc-go-clone-token-result.json
```

`setup` exposes only the base snapshot. `score` copies the candidate into a
temporary clone and overlays the target regression-test files there. It never
overwrites the candidate workspace.

Replay a checked-in MCP transcript against a built server:

```sh
/tmp/agentic-go-eval replay \
  -transcript validation/v0.8.0/tasks/grpc-go-clone-token/replay.json \
  -server /tmp/agentic-go \
  -workspace /tmp/grpc-go-clone-token \
  -artifacts /tmp/grpc-go-clone-token-artifacts \
  -output /tmp/grpc-go-clone-token-replay.json
```

The scorer executes trusted repository tests with the caller's privileges. Its
filesystem boundaries and output limits are containment controls, not a
sandbox.
