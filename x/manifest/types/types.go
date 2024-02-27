package types

func NewStakeHolder(address string, uPercent int32) StakeHolders {
	return StakeHolders{
		Address:    address,
		Percentage: uPercent,
	}
}

func NewStakeHolders(sh ...StakeHolders) []*StakeHolders {
	var stakeHolders []*StakeHolders
	for _, s := range sh {
		s := s
		stakeHolders = append(stakeHolders, &s)
	}
	return stakeHolders
}
