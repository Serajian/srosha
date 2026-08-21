package env

import "github.com/joho/godotenv"

// Load reads the given .env files if they are there, and does not mind if they
// are not: in production the environment comes from the platform and no file
// exists.
//
// It never overwrites a variable that is already set, so the environment always
// wins over a file left behind on a developer's machine.
func Load(paths ...string) {
	for _, p := range paths {
		_ = godotenv.Load(p)
	}
}
