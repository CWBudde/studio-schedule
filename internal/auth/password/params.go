//go:build !js

package password

// defaultParams are the server parameters: the OWASP Password Storage Cheat
// Sheet recommendation for argon2id (19 MiB, two passes, one lane).
var defaultParams = Params{Memory: 19456, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}
