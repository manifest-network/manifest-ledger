package types

import "cosmossdk.io/errors"

var (
	ErrInvalidSKU    = errors.Register(ModuleName, 2, "invalid sku")
	ErrSKUNotFound   = errors.Register(ModuleName, 3, "sku not found")
	ErrUnauthorized  = errors.Register(ModuleName, 4, "unauthorized")
	ErrSKUExists     = errors.Register(ModuleName, 5, "sku already exists")
	ErrInvalidConfig = errors.Register(ModuleName, 6, "invalid module configuration")
)
