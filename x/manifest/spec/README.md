# `manifest` (legacy spec)

> **The canonical reference is now [`../README.md`](../README.md).** These spec stubs are kept for historical context and short pointers; new material should land in the README.

## Abstract

The `x/manifest` module provides:
- Manual minting (`MsgPayout`) — the PoA admin mints fresh coins and disburses to a list of recipient addresses.
- Manual burning (`MsgBurnHeldBalance`) — the PoA admin burns coins held in their own account.

Network inflation is not driven by a bonded ratio as in PoS — it is decided by the chain admin(s). See [`../README.md`](../README.md) for the full operational model.

## Contents

1. **[Concepts](01_concepts.md)** — quick command summary.
2. **[State](02_state.md)** — note that the module is effectively stateless.