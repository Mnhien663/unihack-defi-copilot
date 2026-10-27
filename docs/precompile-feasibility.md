# Custom precompile feasibility

## Observed development environment

- OS: Windows 11 Home Single Language.
- CPU: Intel Core i7-13620H; 16 logical processors.
- Host RAM: approximately 23.7 GiB.
- Available RAM during inspection: approximately 8.6 GiB.
- Free disk: C approximately 59.5 GiB; D approximately 193.3 GiB.
- WSL 2: docker-desktop distribution present.
- Separate development Linux distribution: not installed.
- Docker-visible CPUs: 16.
- Docker-visible memory: 12,366,807,040 bytes, approximately 11.5 GiB.

These are observations, not a guarantee of node capacity.

## Implementation direction

- Upstream: https://github.com/ava-labs/avalanchego
- Subnet-EVM path: graft/subnet-evm.
- Selected release: v1.15.2.
- Pinned commit: 4bb5c1dc13e1b6ff4eaf49fe8674666fbb342a17.
- Root go.mod declares Go 1.26.10.
- Release plugin protocol: 47.
- Version record: precompile/upstream.lock.
- Build compatibility and execution remain unverified.
- Check nested module/workspace requirements before building Subnet-EVM.
- Keep the VM build toolchain separate from the application backend
  when their Go requirements differ.

## Scope

- Core MVP remains on Avalanche Fuji C-Chain, chain ID 43113.
- A custom native precompile requires a custom VM on a separate L1.
- Deploying Solidity contracts on Fuji C-Chain does not install
  a custom native precompile.
- Local proof must be labelled local.
- Fuji L1 deployment requires separate network and execution evidence.
- A protected-flow spending budget must be invoked by the protected
  execution path; it must not be described as blocking all token transfers.

## Open checks and blockers

- Choose and verify a compatible upstream release and Go toolchain.
- Prepare a Linux development environment.
- Measure build and local-network memory/disk usage.
- Verify where WSL/Docker data is stored; free space on D does not
  automatically mean Docker stores its data there.
- Identify a zero-cost Fuji L1 node-hosting plan and test-token needs.
- No custom VM build, local precompile test or Fuji L1 deployment
  has been completed.

## Decision

Proceed with a limited local feasibility spike after core work.
Fuji L1 feasibility remains unconfirmed.
Do not purchase infrastructure without a team decision.