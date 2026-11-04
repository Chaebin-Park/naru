package taskgate

func (g *Gate) evidence(t Task, tip string) error {
	return deny("VERIFICATION_MISSING", "verification support is not activated yet")
}
