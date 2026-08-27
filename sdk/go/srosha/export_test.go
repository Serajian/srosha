package srosha

// KindOf is the sentinel a gRPC code becomes. Tests reach for it so the mapping
// can be checked without a server for every one of them.
var KindOf = kindOf

// NewIdempotencyKey is what Submit calls when the caller supplied none.
var NewIdempotencyKey = newIdempotencyKey
