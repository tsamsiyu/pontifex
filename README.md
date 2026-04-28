# pontifex

Cross-cluster overlay networking for Kubernetes via WireGuard + GoBGP.

A `NetworkOverlay` CRD declares the topology: external **peers** (other clusters)
and internal **edges** (specific pods in this cluster that should be reachable
from peers via stable virtual IPs). Two control planes ship in this monorepo:

- **operator** — reconciles `NetworkOverlay` CRs and provisions the cluster-level
  workloads (RBAC, two gateway Deployments, per-edge-node internal Deployments)
  that run the agent.
- **agent** — runs on every node that hosts a relevant pod or carries a gateway
  label. On gateway nodes it terminates WireGuard and runs GoBGP. On internal
  nodes it runs GoBGP only, installs virtual IPs on its host, and bridges them
  to local pods.

## Layout

This is a multi-module Go workspace.

```
api/                     github.com/tsamsiyu/pontifex/api
apps/agent/              github.com/tsamsiyu/pontifex/apps/agent
apps/operator/           github.com/tsamsiyu/pontifex/apps/operator
```

The `api` module has no internal dependencies, so external clusters can
`go get` just the types if they want to consume the CRD.

## Development

Common tasks are wired up in `Taskfile.yml`:

```sh
task generate         # controller-gen for CRDs + deepcopy
task lint             # golangci-lint across modules
task test             # go test across modules
task build:agent      # bin/agent
task build:operator   # bin/operator
task docker:agent     # build agent image
task docker:operator  # build operator image
task install          # apply operator kustomize bundle
```

## Status

Phase 1 (boilerplate / scaffolding) only. Reconciler bodies, BGP/WG/route
plumbing, and the gateway/edge resolver are intentionally stubbed. See
`.claude/plans/init.md` for the roadmap.
