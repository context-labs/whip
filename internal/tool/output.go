package tool

// Output carries trusted host attachment metadata separately from the guest
// value. The dispatcher never interprets arbitrary JSON keys as attachments.
// The ledger validates ownership, image media and aggregate limits at settlement.
type Output struct {
	Value             any
	ContentReferences []string
}
