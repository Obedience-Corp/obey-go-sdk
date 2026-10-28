# SDK-only persistent HTTP fixture

Run `just test service` from the SDK root. All server files, HTTP request bodies,
child processes and daemon state stay in disposable containers. Do not start this
fixture server directly on the host.

The implementation demonstrates an application-owned fsync/rename job journal,
key-before-send ordering, process restart and exact-ref recovery. `GET /jobs/ID/wait`
uses only the HTTP caller lifetime. `POST /jobs/ID/cancel` is a separate explicit
control. `POST /jobs/ID/retry` requires the original saved key/input and never runs
automatically. Missing optional key metadata permits read-only recovery but refuses
resubmission. Approval responses require an explicit boolean.

See `docs/integration-evidence.md` at the SDK root for the verified journey and its
provider/platform limits. This is a local fixture without production authentication.
