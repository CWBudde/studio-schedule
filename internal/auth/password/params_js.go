//go:build js

package password

// defaultParams are the demo parameters. The WASM demo runs in a single
// browser thread, keeps its data in memory only and holds no real accounts,
// so the server parameters would only make sign-up and login sluggish there.
// Hashes record their parameters, so these hashes still verify on the server
// (where NeedsRehash reports them for an upgrade) and vice versa.
var defaultParams = Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}
