<!--
order: 2
-->

# State

## State Objects

The `x/manifest` module only manages the following object in state: Params. This object holds all the required information for the module to operate. For automatic inflation, the x/mint modules `Params` is used to take the per year reward coins and divide it by the number of blocks per year, resulting in the per block reward amount.

```proto
message Params {
  option (amino.name) = "manifest/params";
  option (gogoproto.equal) = true;
  option (gogoproto.goproto_stringer) = false;

  repeated StakeHolders stake_holders = 1;

  Inflation inflation = 2;
}

// StakeHolders is the list of addresses and their percentage of the inflation distribution
message StakeHolders {
  option (gogoproto.equal) = true;

  // manifest address that receives the distribution
  string address = 1;

  // percentage is the micro denom % of tokens this address gets on a distribution.
  // 100% = 100000000 total. so 1000000 = 1%.
  int32 percentage = 2;
}

// Inflation is the distribution coins to the stake holders
message Inflation {
  option (gogoproto.equal) = true;

  // if auto payouts are done per block
  bool automatic_enabled = 1;

  // amount of umfx tokens distributed per year
  uint64 yearly_amount = 2;

  // the token to distribute (i.e. 'umfx')
  string mint_denom = 3;
}
```

## State Transitions

The following state transitions are possible:

- Update params for stakeholders percent, and inflation values (denom, and automatic)