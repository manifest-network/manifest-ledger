# `manifest`

## Abstract

This document specifies the internal `x/manifest` module of The Lifted Initiative, Manifest Ledger.

The `x/manifest` module provides the following abilities:
- Setting a group of stakeholders that receive a set % of inflation
- Toggling Inflation from automatic and manual modes

This inflation is not tied to a standard bonded ratio like typical proof-of-stake (PoS) systems, Instead it is up to chain admin(s) to decide given the nature of a proof-of-authority (PoA) chain.

By using this module, token distributions can be delivered to select stakeholders within the bounds of certain set parameters

## Contents

1. **[Concepts](01_concepts.md)**
2. **[State](02_state.md)**