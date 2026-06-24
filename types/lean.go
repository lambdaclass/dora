package types

// NamedValidator pairs a validator index with an optional display name. Used by
// the bitfield/participation formatters when rendering attestation aggregation
// bits in templates.
type NamedValidator struct {
	Index uint64
	Name  string
}
