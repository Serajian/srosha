package database

// maxPoolConns is a bound, not a knob. Postgres allows a hundred connections by
// default, so anything near this is a typo -- and a larger number would wrap
// when converted to the driver's int32.
const maxPoolConns = 1000
