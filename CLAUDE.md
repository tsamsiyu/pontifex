# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Pontifex is cross-cluster overlay networking for Kubernetes via WireGuard + GoBGP. A `NetworkOverlay` CRD (cluster-scoped, `pontifex.io/v1alpha1`) declares external **peers** (other clusters) and internal **edges** (local pods reachable from peers via stable virtual IPs). Two control planes ship in this monorepo: the **operator** (reconciles CRs, provisions cluster workloads) and the **agent** (runs on gateway and internal nodes; handles WG/BGP/routes/firewall).

**Status:** Phase 1 scaffolding only. Most reconciler bodies, BGP/WG/route plumbing, and resolver logic are intentionally stubbed and return `errors.New("not implemented")` or zero values. The full multi-phase roadmap, design rationale, and type-set notes live in `.claude/plans/init.md` — read it before making non-trivial changes; many decisions there are user-confirmed and not re-derivable from the code.

## Commands

Tooling is wired through `Taskfile.yml` (use `task`, not raw `go`/`docker`/`kubectl`). `controller-gen` and `golangci-lint` are auto-installed into `./bin/` on first use.

```sh
task generate         # controller-gen: deepcopy + CRD manifests into apps/operator/config/crd/bases
task lint             # golangci-lint run ./... in each module
task test             # go test ./... in each module
task build:agent      # → bin/agent
task build:operator   # → bin/operator
task docker:agent     # AGENT_IMAGE=... task docker:agent to override tag
task docker:operator
task install          # kubectl apply -k apps/operator/config/default
task uninstall
```

Run a single test in one module: `cd apps/agent && go test ./internal/cluster -run TestMediator`. The Taskfile loops over modules with per-module `cd`, so test/lint commands won't pick up a top-level `go test ./...`.

After editing types in `api/v1alpha1/`, always run `task generate` — `zz_generated.deepcopy.go` and the CRD YAML in `apps/operator/config/crd/bases/` are checked-in artifacts, not gitignored.

## Architecture

This is a **multi-module Go workspace** (`go.work`) with three modules under module path `github.com/tsamsiyu/pontifex`:

- `api/` — CRD types only, **zero internal deps** so external clusters can `go get` just the types. Don't add imports from `apps/` here.
- `apps/operator/` — controller-runtime operator scaffolded with kubebuilder.
- `apps/agent/` — node-local daemon, runs in two modes selected by `--mode=gateway|internal`.

### Operator pipeline

`cmd/operator/main.go` runs as a single-replica Deployment (`LeaderElection: false` — reintroduce before scaling). Five concerns, each in its own package under `internal/`:

- `controller/networkoverlay_controller.go` — top-level reconciler. Per CR: allocate community, generate WG keypair (Secret in operator namespace), ensure cluster infra (RBAC, gateway Deployments, per-node internal Deployments), resolve edges. Adds finalizer `pontifex.io/networkoverlay` for per-overlay cleanup.
- `community/` — allocates `<clusterASN>:<n>` BGP communities into `status.community` (cluster ASN comes from `OperatorConfig`, not the CR).
- `wgkeys/` — generates per-overlay WG keypairs; private key lives in `Secret/pontifex-wg-<overlay>` mounted into gateway pods, public key published to `status.publicKey`.
- `gateways/` — separate two-stage pipeline (`observer.go` → `overlay_updater.go`) that watches Nodes by gateway label and patches `status.gateways` on every NetworkOverlay. Runs alongside the controller; both write to different status paths.
- `resolver/` — watches Pods+Nodes, parses `spec.edges[].podLabelsSelector` via `labels.Parse`, populates `status.edges`. Skips edges resolving to gateway-labeled nodes (gateway/internal modes can't share a node).
- `deploy/` — workload templates: `rbac.go` (agent SA: `get/list/watch` on NetworkOverlay only), `gateway_deploy.go` (two single-replica Deployments — primary, secondary; **not** DaemonSets), `internal_deploy.go` (per-node Deployment lifecycle keyed off `union(status.edges[].nodeName) \ gatewayLabeledNodes`).

### Agent pipeline

`cmd/agent/main.go` parses `--mode`, loads either `GatewayConfig` or `InternalNodeConfig` (no shared `AgentConfig`, no nil-checks), and dispatches to one of two managers — no runtime branching inside reconcilers.

The shared shape inside each manager:

```
cluster.Observer (informer over NetworkOverlay)
    → cluster.Mediator (debounces ~500ms, fans out []NetworkOverlay snapshots)
    → reconcilers (each subscribes independently, retries independently)
```

`Mediator` is a pure pipe: no business logic, no state diffing, no error-driven backoff. Each reconciler runs its own retry loop against the latest snapshot.

**Reconcilers are silly idempotent state appliers**: each cycle they (1) `List*` what they currently manage on the host, (2) compute desired set from the snapshot, (3) reconcile the diff. No event handling, no shared in-memory state, no cross-reconciler teardown coordination. Transient errors during teardown (e.g. routes reconciler removing a VRF before BGP drops neighbors) are accepted; the next cycle is clean.

Per-mode reconciler sets:
- Gateway: `bgp/gateway` (eBGP to peers over WG, iBGP route-reflector to internals, community import/export filters, secondary ASN prepend), `routes/gateway` (global RIB), `wireguard` (one iface per overlay).
- Internal: `bgp/internalnode` (iBGP to all `status.gateways`, advertises `EdgeStatus.VirtualIP/32` with overlay community, import policy splits routes into per-overlay VRFs), `routes/internalnode` (VRFs + virtual IPs + firewall DNAT/SNAT bridge to local pod IP).

Cross-reconciler: BGP exposes `<-chan RouteEvent` per overlay that the routes reconciler consumes for live route deltas; topology still flows through the mediator.

### libs/ contracts

Each `libs/<x>/interface.go` is the contract; the file beside it is the impl. Reconcilers only touch interfaces (test fakes swap in cleanly). Every lib that mutates host state exposes `List*` methods filtered to **only entities this agent manages** via stable markers — this is what makes the silly-reconciler pattern safe:

- **Name prefix `pntfx-`** — WG interfaces, VRF master devices, VRF dummies. `libs/wg.ListInterfaces` and `libs/routes.ListVRFs` filter on it.
- **`proto pontifex`** — custom routing-protocol identifier registered in `/etc/iproute2/rt_protos.d/pontifex.conf` (agent writes this on startup if missing). All routes installed by `libs/routes` set this proto; `ListRoutes` filters on it.
- **`pontifex:overlay=<name>:vip=<ip>` comment** — on every iptables/nftables rule and chain inserted by `libs/firewall`. `ListBridges` parses these back.

If you add new host-mutating operations, follow these conventions or the diff-and-prune logic will either miss orphans or stomp on unrelated state.

### Data flow rules to preserve

- The agent has **`get/list/watch` on NetworkOverlay only**. It never writes back. All status updates go through the operator.
- `status.gateways` and `status.edges` are operator-owned; agents read them as authoritative for placement.
- WG private keys are **not** in agent config — they're mounted from `Secret/pontifex-wg-<overlay>` at `<WGKeyDir>/<overlay>/private` and read at reconcile time.
- Cluster ASN lives in `OperatorConfig` (env `PONTIFEX_ASN`), **not** on the CR. Don't add it to the spec.
- `NetworkOverlayStatus.Conditions` is intentionally absent in v1alpha1 — additive change for later, don't introduce it as part of unrelated work.
