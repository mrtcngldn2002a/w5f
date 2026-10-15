package discover

// FamilyNames lists Deep random's own shelves, in the order they were added
// (the source audit draws from each in turn).
func FamilyNames() []string {
	var out []string
	for _, f := range families {
		out = append(out, f.Name())
	}
	return out
}

// FamilyByName is one of Deep random's own shelves; nil when there is none.
func FamilyByName(name string) Family {
	for _, f := range families {
		if f.Name() == name {
			return f
		}
	}
	return nil
}
