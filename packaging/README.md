# Wahoo Mihomo 1.19.33

Upstream: MetaCubeX/mihomo `v1.19.33`, commit
`3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8`. GPL-3.0 source is retained in
this fork. This is an intentionally patched core, not an official release mirror.

## Runtime delta

Only `adapter/outboundgroup/urltest.go` changes production Go behavior. Reuse
`tolerance` (Wahoo configs use 30): uniformly choose from every healthy, measured
node within `minRTT + tolerance`. Keep a healthy current node even outside the
updated pool. Failure or provider removal triggers a new choice. A same-name
provider replacement adopts the new proxy object. Missing/failed RTT samples do
not enter the pool. No successful samples retains upstream first-proxy behavior
without pinning that provisional choice. Empty providers retain `empty-fallback`.
The explicit `fixed` API override retains its upstream semantics.

Selection uses a mutex rather than the upstream ten-second result cache, so
confirmed health changes and provider versions are visible at the next request.
Existing connections, health-check scheduling, dial failure thresholds, DNS,
transports and all other group types are unchanged. The patch costs a provider
lookup and a short membership scan per automatic selection; new pools are built
only when a new selection is needed. Future upstream merges must recheck health
sample semantics, provider identity, and url-test override behavior. Run the pool,
provider replacement and race tests before rebasing.

## Build and immutable distribution

`packaging/toolchain.lock.json` pins the checksum of the upstream workflow's
MetaCubeX Go 1.26 toolchain. Preserve `with_gvisor`, CGO=0, trimpath and upstream
linker settings for static Linux arm64 (Android's existing executable launcher)
and Windows amd64-v1. This does not introduce a new FFI ABI. Keep the source CA
bundle for reproducibility rather than injecting the runner's current CA bytes.

The existing `wahoo-soft/v2ray-mihomo` packaging repository runs
`wahoo-mihomo-core.yml`, checks out an exact commit of this fork, runs race tests,
builds both targets, publishes archives + manifest + GPL license to
`registry.wahooai.com/v2ray/wahoo-mihomo`, resolves its immutable OCI digest and
generates both app locks. Credentials remain in the packaging repository.
No branch, floating tag, or latest reference is accepted by consumers.

App builds use `repository@sha256:...` and verify archive and executable hashes
and lengths before atomically replacing their packaged core. Never fabricate a
lock before a successful publish. The source manifest records both hashes,
source commits, toolchain and build parameters.

## Promotion and rollback

The new core is opt-in through each app's reviewed build lock, with debug/internal
builds before public app release. There is no new runtime feature flag or API:
the compiled core revision is the behavior gate. Revert the app lock and downloader
together to roll back to the previous official core; retain previous OCI artifacts.
Record source commit and digest in build summaries; use existing core logs and
connection/failure telemetry during QA. Do not claim real-tunnel verification from
unit tests or compilation. Required Android VpnService and Windows Direct tests:
DNS, IPv4/IPv6, TCP, UDP/QUIC, Reality, XHTTP H2/H3, local rule sets, reconnect,
network changes and sleep/wake, plus healthy sticky selection and failed-node
recovery. Windows Store Leaf and Apple sing-box are outside this release.
