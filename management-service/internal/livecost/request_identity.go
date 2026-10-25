package livecost

import "reflect"

// SameRequestIdentity fences the actual supplier work, not the price or audit
// label in force when a retry is observed. A changed setting never authorizes
// a second send, reprices the old reservation, or weakens content/scope checks.
func SameRequestIdentity(left, right Request) bool {
	left.PricingSnapshot, right.PricingSnapshot = nil, nil
	left.EstimatedMicros, right.EstimatedMicros = 0, 0
	left.Reason, right.Reason = "", ""
	return reflect.DeepEqual(left, right)
}
