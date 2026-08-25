package scheduler

// Every is the descriptor form of a plain interval, and the reason a schedule
// is a string here rather than a duration: "@every 5m" and "*/5 * * * *" and
// "0 3 * * *" all go through the same parser, so a deployment that outgrows an
// interval does not need a new setting.
//
// One second is the finest interval there is. Anything shorter is rounded up to
// it rather than refused, so a schedule of "@every 100ms" runs ten times less
// often than it reads -- worth knowing before writing one.
const Every = "@every "
