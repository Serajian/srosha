package shared

// Cursor is where a listing should carry on from. After is nil for the first
// page. It works on IDs because a ULID sorts by creation time, so one column
// gives both the order and the position.
//
// Not an offset: an offset makes the database count and throw away everything
// before the page, and it skips or repeats rows when the set changes underneath.
type Cursor struct {
	After *ID
	Limit int
}

// Normalize fills in the default and clamps to the ceiling, so a zero value
// means "the first page", never "nothing".
func (c Cursor) Normalize() Cursor {
	if c.Limit <= 0 {
		c.Limit = DefaultPageSize
	}
	if c.Limit > MaxPageSize {
		c.Limit = MaxPageSize
	}
	return c
}

// Pagination is one page, and where the next one starts.
type Pagination[T any] struct {
	Items      []*T
	NextCursor *ID
}

// HasNext is derived rather than stored, so it cannot disagree with the cursor.
// As a second field it would be set by hand, and forgetting it once makes a
// caller stop paging while rows are still waiting.
func (p Pagination[T]) HasNext() bool { return p.NextCursor != nil }
