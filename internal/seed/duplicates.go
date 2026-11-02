package seed

// duplicates adds the school's duplicate pairs.
func (b *builder) duplicates() {
	for _, p := range b.s.duplicates {
		b.person(p, "", "")
	}
}
