# Runner protocol

This reference describes how to build a persistent runner that communicates with
bktec. The runner boots once, pulls batches, executes them and reports results.
bktec owns work selection, test retries, muting and the final job outcome.
The host is not wired into the CLI yet; `bktec run` is unchanged.

## Connect and register

Read `BUILDKITE_TEST_ENGINE_RUNNER_SOCKET` from the environment and connect using
HTTP/1.1 over that Unix socket. Send UTF-8 JSON with `Content-Type: application/json`.
bktec creates the private mode-0600 socket; the runner must not create or remove it.
Unknown additive JSON fields should be ignored. All wire durations are milliseconds.

After boot, send `POST /v1/sessions` with a unique instance ID for this process:

```json
{
  "instance_id": "unique-per-process",
  "runner": {
    "name": "buildkite-rspec", "version": "0.1.0",
    "framework": "rspec", "framework_version": "3.13.6",
    "language": "ruby", "language_version": "3.3.12", "pid": 4242
  },
  "capabilities": {"selector_formats": ["selector", "file", "example"]}
}
```

The response is `201 {"session_id":"...","poll":{"max_wait_ms":30000}}`.
Include `X-Bktec-Session: <session_id>` on every subsequent request. Advertise a
nonempty list of selector formats you support; bktec only dispatches those formats.
Use a read timeout greater than `poll.max_wait_ms` (for example, add 5 seconds).

## Pull and execute

Send `POST /v1/batches` with `{}`. A `200` response is one of:

```json
{"type":"batch","batch":{"id":"b_1","tests":[{"format":"file","path":"spec/example_spec.rb"}],"timeout_ms":600000}}
```
```json
{"type":"wait","retry_after_ms":1000}
```
```json
{"type":"done","reason":"plan_completed"}
```

- **batch:** execute the supplied tests, then report the result before pulling again.
  RSpec maps `selector` to `value`, `file` to `path`, and `example` to `identifier`
  with `path` fallback. Additional test metadata may be present.
- **wait:** sleep for `retry_after_ms`, then pull again. Responses may return
  immediately; `max_wait_ms` is an upper bound, not a required hold.
- **done:** flush collector data and exit zero. Reasons are `plan_completed`,
  `pool_consumed`, `pool_errored`, `terminating` and `error`; bktec decides job status.

There is one outstanding batch per session. Its timeout starts at host dispatch,
not when execution begins, and includes reporting until bktec accepts the result.
Reset per-batch framework state so filters, reporters and examples do not leak.
Only execute assigned tests; do not independently retry or suppress failures.

## Report results

Send `POST /v1/batches/{id}/results` with one of these envelopes (RSpec examples):

```json
{"status":"completed","report_format":"rspec-json","report":{"examples":[],"summary":{"example_count":0}}}
```
```json
{"status":"errored","report_format":"rspec-json","error":{"kind":"runner_error","message":"report file missing"}}
```

`report_format` must be a nonempty string identifying the report encoding. The
transport passes it through without choosing a parser or restricting the runner's
name, framework or language. The calling command must support that format and
validate its contents; transport acceptance does not imply parser support.
An unsupported format or unusable report must fail the command, not be treated as
passing because the HTTP request succeeded or the runner exited zero.
Completed reports must be JSON objects; errored envelopes carry an error instead.

`completed` means a native report was produced, not that tests passed: send the
unchanged report including failures. Use `errored` when execution/report generation
fails without a usable report. Success returns `200 {}`. Requests are limited to
64 MiB. Collector uploads are separate from this exchange; record each actual
example execution once, including host-requested retries.

## Replay and errors

- Retry a lost handshake response with the **same instance ID** to recover its session.
  A new instance retires the old session and makes its outstanding work unresolved.
- Repeating a pull replays the outstanding batch **without extending its deadline**.
  Do not execute a replayed batch again if execution already started or finished.
- Cache the encoded result body: byte-identical reposts succeed without redelivery;
  different bodies for the same accepted batch return **409**, even for whitespace changes.
- **400:** malformed request or unsupported selector capability. **401:** missing,
  unknown or retired session—stop using it. **409:** conflicting/non-outstanding result
  or handshake during shutdown. **426:** unsupported version, with `{"supported":[1]}`.

Replay state is in-memory, not durable delivery. A restarted runner uses a new
instance ID; retired instances cannot reclaim their old sessions.

## Shutdown and deadlines

On SIGINT/SIGTERM, stop after the current example, flush collector data, send
`DELETE /v1/sessions/{session_id}` with `{"reason":"signal_term"}`, and exit.
The reason may be any nonempty string; success returns `200 {}`. Do not submit a
partial batch as completed. Keep polling waits interruptible so shutdown is prompt.

The current host defaults to **5 minutes** for boot/handshake, **10 minutes** per
batch (use the supplied `timeout_ms`), and **90 seconds** for shutdown/flush.
Timeouts can be configured by the host; these are provisional, not Rails-calibrated
defaults. On timeout bktec terminates the process group, escalating to SIGKILL after
the shutdown grace. After `done`, it waits for runner exit before cleaning up.

For context, the minimal RSpec integration fixture measured 192 ms to first pull
and 34/6/3 ms per batch on Linux x64, Go 1.26.0 (race-enabled), Ruby 3.1.2 and
RSpec 3.13.6 (2026-09-23). Uploads were captured locally; **this is not Rails boot
or network-flush timing**. Measure your application's boot, batches and flushes
before choosing production timeout budgets.
