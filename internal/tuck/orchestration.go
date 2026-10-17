package tuck

// Delivery facts are supplied by the existing Manager loop. The policy neither
// reads storage nor mutates tasks, queues, permissions, or model content.
type deliveryFacts struct {
	Reason, SourceKind, TurnID    string
	Enabled, Permitted, Delivered bool
}

func (f deliveryFacts) eligible() bool {
	return f.Reason == "completed" && f.SourceKind != "delivery_required" && f.Enabled && f.Permitted
}
func deliveryFollowup(f deliveryFacts) string {
	if !f.eligible() || f.Delivered {
		return ""
	}
	return f.TurnID
}
