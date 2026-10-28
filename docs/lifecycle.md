# Client and run lifecycle

Construct with `NewClient(path, options...)` or `NewClientFromPath(options...)`.
Construction checks the executable and clones configuration without daemon or
provider calls. Each operation receives its caller context. `WithCamp` chooses
a default explicit scope; per-call Camp overrides it. Otherwise the CLI infers
scope from child cwd (`WithWorkingDirectory`). Agent workspace
(`SessionOptions.WorkingDirectory`, forwarded as `--workdir`) is separate.

Session defaults, JSON config, environment maps, per-call files and timeout values
are copied. Finish preparing an option before passing it; callers may reuse or
change their own options after StreamPrompt returns. The client configuration
is immutable and operations may run concurrently. Environment overrides replace
inherited keys in the child and never change the parent environment. Prompts are
passed byte-for-byte on stdin, never argv or a shell. Relative files resolve from
the child cwd under existing CLI attachment rules.

RunPrompt drains the complete stream and returns a typed result/error.
StreamPrompt currently selects the advertised legacy run-v1 invocation without
`--protocol-version`, and reports final-only fidelity. RequireLiveEvents rejects
v1 before creating a session. Nil timeout keeps the CLI’s 30-minute default; pointer
zero disables it; negative values fail before spawning. Caller cancellation
always remains effective. Doctor health failure does not invalidate a valid CLI
capability document; discovery uses a bounded 30-second child.

Use one consumer for Next/Result. Event and Result return independent snapshots;
Err returns a typed *Error even when Result is not called. Result drains unread
records and waits for child exit, then caches its validated result. Repeated
Result calls return copies. Concurrent Next/Result returns stream_misuse.

Cancellation signals only the dedicated CLI child, permits its cleanup for the
configured grace (default 10 seconds), then kills/reaps it. Result continues draining
during grace after Next returns false; it can retain confirmed stopped cleanup
and partial session identity with a cancellation error. Missing confirmation
remains cleanup unknown. Close is idempotent and safe concurrently with a blocked
Next; Client.Close releases its children and rejects new operations. The client
does not signal the shared daemon or independently stop persistent work.

Per-record output and final content have separate 64 MiB defaults; stderr retains a
64 KiB tail and queues hold 16 records. Overflow is an error, not truncation. Slow
consumers exert backpressure. During cancellation, abandoned queues are released
at the grace boundary; no cleanup success is inferred from a killed child.
Windows builds, but equivalent native graceful cleanup is not yet supported or
verified. This baseline does not claim durable session/turn APIs or publication.
