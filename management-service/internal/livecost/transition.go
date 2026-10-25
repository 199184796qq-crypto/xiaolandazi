package livecost

import "fmt"

type Entry struct {
	Request        Request `json:"request"`
	Receipt        Receipt `json:"receipt"`
	Status         string  `json:"status"`
	ReservedMicros int64   `json:"reserved_micros"`
	ChargedMicros  int64   `json:"charged_micros"`
	UsageSource    string  `json:"usage_source"`
}

// ApplyReceipt is monotonic: unknown is never refunded, and weaker evidence
// cannot replace supplier usage or an invoice. Duplicate acknowledgements are no-ops.
func ApplyReceipt(entry Entry, receipt Receipt) (Entry, error) {
	if err := ValidateReceipt(receipt); err != nil {
		return entry, err
	}
	if receipt.TenantID != entry.Request.TenantID || receipt.RoomID != entry.Request.RoomID {
		return entry, ErrRequestConflict
	}
	oldID := entry.Receipt.ProviderRequestID
	if oldID != "" && receipt.ProviderRequestID != "" && oldID != receipt.ProviderRequestID {
		return entry, ErrReceiptConflict
	}
	if receipt.ProviderRequestID == "" {
		receipt.ProviderRequestID = oldID
	}
	// Keep the actual sent-text reference units when stronger supplier evidence
	// arrives later. Supplier Characters remains its own distinct measurement.
	if receipt.CharacterRule == "" && entry.Request.Kind == KindTTS && entry.Request.CharacterRule != "" {
		receipt.CharacterRule = entry.Request.CharacterRule
		receipt.BillableCharacters = entry.Request.BillableCharacters
		receipt.SentTextCharacters = entry.Request.Characters
	}
	if receipt.Provider == "" {
		receipt.Provider = entry.Receipt.Provider
	}
	if receipt.Model == "" {
		receipt.Model = entry.Receipt.Model
	}
	switch receipt.Status {
	case ReceiptUnknown:
		if entry.Status == StatusReleased || entry.Status == StatusDeclined {
			return entry, ErrReceiptConflict
		}
		if entry.Status == StatusSettled {
			if entry.Receipt.ProviderRequestID == "" && receipt.ProviderRequestID != "" {
				entry.Receipt.ProviderRequestID = receipt.ProviderRequestID
			}
			return entry, nil
		}
		entry.Status = StatusUnknown
		entry.Receipt = receipt
		return entry, nil
	case ReceiptNotSent:
		if entry.Status == StatusReleased {
			return entry, nil
		}
		if entry.Status != StatusReserved {
			return entry, ErrReceiptConflict
		}
		entry.Status = StatusReleased
		entry.ReservedMicros = 0
		entry.Receipt = receipt
		return entry, nil
	case ReceiptSuccess:
		if entry.Status == StatusReleased || entry.Status == StatusDeclined {
			return entry, ErrReceiptConflict
		}
		source := receipt.UsageSource
		if source == "" {
			source = SourceSentTextEstimate
		}
		receipt.UsageSource = source
		cost := entry.Request.EstimatedMicros
		if receipt.ActualKnown {
			cost = receipt.ActualMicros
		} else if receipt.EstimatedMicros != nil {
			cost = *receipt.EstimatedMicros
		}
		if entry.Status == StatusSettled {
			oldRank, newRank := sourceRank(entry.UsageSource), sourceRank(source)
			if newRank < oldRank {
				return entry, nil
			}
			if newRank == oldRank {
				if cost != entry.ChargedMicros {
					return entry, fmt.Errorf("%w: 同等级费用不一致", ErrReceiptConflict)
				}
				if entry.Receipt.ProviderRequestID == "" && receipt.ProviderRequestID != "" {
					entry.Receipt.ProviderRequestID = receipt.ProviderRequestID
				}
				return entry, nil
			}
		}
		entry.Status = StatusSettled
		entry.ReservedMicros = 0
		entry.ChargedMicros = cost
		entry.UsageSource = source
		entry.Receipt = receipt
		return entry, nil
	}
	return entry, ErrReceiptConflict
}
func sourceRank(source string) int {
	switch source {
	case SourceSupplierInvoice:
		return 3
	case SourceSupplierUsage:
		return 2
	case SourceSentTextEstimate:
		return 1
	}
	return 0
}
