package logincode

import "time"

// Lifetime is how long a code is worth typing. Minutes rather than hours,
// because the window is exactly how long a code read over somebody's shoulder
// or left in an inbox stays usable.
const Lifetime = 10 * time.Minute

// MaxGuesses is how many wrong answers one code survives. Six digits is a
// million values, which a script exhausts in seconds without a limit.
const MaxGuesses = 3

// digits is how long a code is. Six because a person types it from an email,
// and the three rules around it are what make it safe rather than its length.
const digits = 6
