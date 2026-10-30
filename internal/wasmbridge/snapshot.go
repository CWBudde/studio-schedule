package wasmbridge

// SnapshotBuild returns the build the demo saves its snapshot under and
// restores it with (memory.Store.Snapshot and Restore): the app's version
// and the demo school the data was seeded for, e.g. "v1.2.3 (werkstatt)".
// So a snapshot of another school is refused like one of another version
// (memory.ErrSnapshotMismatch), and the demo starts from that school's
// seed. Snapshots saved before the demo had schools carry the plain
// version, which no school's build equals.
func SnapshotBuild(version, school string) string {
	return version + " (" + school + ")"
}
