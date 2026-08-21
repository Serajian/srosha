// Package config turns the process environment into the two binaries' settings.
//
// It is read by bootstrap and by nothing else. The core takes plain values --
// a duration, a policy, a limit -- so it never learns where they came from.
package config

import "github.com/Serajian/srosha/pkg/env"

// prefix keeps our keys apart from everything else in the environment.
const prefix = "NOTIF_"

// envFiles is what each binary reads before looking at the environment: the
// shared file first, then its own. Neither has to exist -- in production the
// platform supplies the variables and no file is there.
func envFiles(binary string) []string {
	return []string{".env", ".env." + binary}
}

func reader(binary string) *env.Reader {
	env.Load(envFiles(binary)...)
	return env.New(prefix)
}
