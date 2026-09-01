package types

import "cosmossdk.io/errors"

// Registered billing module errors.
var (
	ErrInvalidParams          = errors.Register(ModuleName, 1, "invalid params")
	ErrLeaseNotFound          = errors.Register(ModuleName, 2, "lease not found")
	ErrLeaseNotActive         = errors.Register(ModuleName, 3, "lease not active")
	ErrInsufficientCredit     = errors.Register(ModuleName, 4, "insufficient credit balance")
	ErrMaxLeasesReached       = errors.Register(ModuleName, 5, "maximum leases per tenant reached")
	ErrUnauthorized           = errors.Register(ModuleName, 6, "unauthorized")
	ErrReserved7              = errors.Register(ModuleName, 7, "reserved") // Reserved for future use
	ErrCreditAccountNotFound  = errors.Register(ModuleName, 8, "credit account not found")
	ErrInvalidLease           = errors.Register(ModuleName, 9, "invalid lease")
	ErrSKUNotFound            = errors.Register(ModuleName, 10, "sku not found")
	ErrSKUNotActive           = errors.Register(ModuleName, 11, "sku not active")
	ErrProviderNotFound       = errors.Register(ModuleName, 12, "provider not found")
	ErrProviderNotActive      = errors.Register(ModuleName, 13, "provider not active")
	ErrMixedProviders         = errors.Register(ModuleName, 14, "all SKUs in a lease must belong to the same provider")
	ErrNoWithdrawableAmount   = errors.Register(ModuleName, 15, "no withdrawable amount")
	ErrEmptyLeaseItems        = errors.Register(ModuleName, 16, "lease must contain at least one item")
	ErrInvalidQuantity        = errors.Register(ModuleName, 17, "quantity must be greater than zero")
	ErrDuplicateSKU           = errors.Register(ModuleName, 18, "duplicate sku in lease items")
	ErrInvalidCreditOperation = errors.Register(ModuleName, 19, "invalid credit operation")
	ErrArithmeticOverflow     = errors.Register(ModuleName, 20, "billing arithmetic overflow")
	// Deprecated: use ErrArithmeticOverflow. ErrReserved20 is retained as a
	// source-compatible alias; code 20 now has a concrete meaning.
	ErrReserved20                            = ErrArithmeticOverflow
	ErrTooManyLeaseItems                     = errors.Register(ModuleName, 21, "too many items in lease")
	ErrLeaseNotPending                       = errors.Register(ModuleName, 22, "lease not in pending state")
	ErrMaxPendingLeasesReached               = errors.Register(ModuleName, 23, "maximum pending leases per tenant reached")
	ErrInvalidRejectionReason                = errors.Register(ModuleName, 24, "invalid rejection reason")
	ErrInvalidRequest                        = errors.Register(ModuleName, 25, "invalid request")
	ErrInvalidClosureReason                  = errors.Register(ModuleName, 26, "invalid closure reason")
	ErrInvalidMetaHash                       = errors.Register(ModuleName, 27, "invalid meta hash")
	ErrInvalidServiceName                    = errors.Register(ModuleName, 28, "invalid service name")
	ErrInvalidCustomDomain                   = errors.Register(ModuleName, 29, "invalid custom domain")
	ErrCustomDomainAlreadyClaimed            = errors.Register(ModuleName, 30, "custom domain already claimed")
	ErrLeaseNotEditable                      = errors.Register(ModuleName, 31, "lease state does not allow custom_domain edit")
	ErrLeaseItemNotFound                     = errors.Register(ModuleName, 32, "lease item not found")
	ErrAmbiguousLeaseItem                    = errors.Register(ModuleName, 33, "lease item lookup is ambiguous")
	ErrLeaseAcknowledgementDeadlineExceeded  = errors.Register(ModuleName, 34, "lease acknowledgement deadline exceeded")
	ErrLeaseAcknowledgementActiveCapExceeded = errors.Register(ModuleName, 35, "lease acknowledgement active cap exceeded")
	ErrReservationInvariant                  = errors.Register(ModuleName, 36, "billing reservation invariant violated")
	ErrLeaseQueryLimitExceeded               = errors.Register(ModuleName, 37, "tenant lease query limit exceeded")
	ErrReservationDenomLimitExceeded         = errors.Register(ModuleName, 38, "credit account reservation denomination limit exceeded")
)

// MaxItemsPerLeaseHardLimit is the absolute maximum number of items per lease.
// This is a hard limit enforced at the message validation level to prevent
// denial-of-service attacks. The configurable max_items_per_lease param
// must be <= this value.
const MaxItemsPerLeaseHardLimit = 100

// MaxReservedDenomsPerCreditAccount bounds the size of the aggregate coin
// slices rewritten on every reservation lifecycle transition. Historical
// accounts above the limit remain releasable, but new leases cannot increase
// their denomination cardinality.
const MaxReservedDenomsPerCreditAccount = 1000

// MaxQuantityPerItem is the maximum quantity per lease item.
// This bounds per-item work and state size. Prices are independently bounded by
// math.Int, so all price/quantity arithmetic must still use checked operations.
const MaxQuantityPerItem uint64 = 1_000_000_000 // 1 billion

// MaxRejectionReasonLength is the maximum encoded byte length of a rejection reason.
const MaxRejectionReasonLength = 256

// MaxClosureReasonLength is the maximum encoded byte length of a closure reason.
const MaxClosureReasonLength = 256

// MaxMetaHashLength is the maximum length of a meta_hash field.
// 64 bytes accommodates SHA-256 (32 bytes) and SHA-512 (64 bytes) hashes.
const MaxMetaHashLength = 64

// MaxServiceNameLength is the maximum length of a service_name field.
// Matches DNS label maximum length (RFC 1123).
const MaxServiceNameLength = 63

// MaxCustomDomainLength is the maximum total length of a LeaseItem.custom_domain
// FQDN. Matches the RFC 1035 maximum length for a domain name.
const MaxCustomDomainLength = 253
