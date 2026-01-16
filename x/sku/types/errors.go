package types

import "cosmossdk.io/errors"

var (
	ErrInvalidSKU       = errors.Register(ModuleName, 1, "invalid sku")
	ErrSKUNotFound      = errors.Register(ModuleName, 2, "sku not found")
	ErrUnauthorized     = errors.Register(ModuleName, 3, "unauthorized")
	ErrInvalidConfig    = errors.Register(ModuleName, 4, "invalid module configuration")
	ErrInvalidProvider  = errors.Register(ModuleName, 5, "invalid provider")
	ErrProviderNotFound = errors.Register(ModuleName, 6, "provider not found")
	ErrInvalidAPIURL    = errors.Register(ModuleName, 7, "invalid API URL")
)

// Validation constants for provider and SKU fields
const (
	// MaxAPIURLLength is the maximum length of an API URL.
	MaxAPIURLLength = 2048

	// MaxSKUNameLength is the maximum length of a SKU name.
	MaxSKUNameLength = 256

	// MaxMetaHashLength is the maximum length of a metadata hash in bytes.
	// Set to 64 to accommodate SHA-512 and similar hash algorithms.
	MaxMetaHashLength = 64
)
