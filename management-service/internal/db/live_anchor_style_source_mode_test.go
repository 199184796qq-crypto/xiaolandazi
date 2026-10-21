package db

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"
)

type styleModeTestRow []driver.Value

func (r styleModeTestRow) Scan(dest ...any) error {
	if len(dest) != len(r) {
		return fmt.Errorf("dest %d values %d", len(dest), len(r))
	}
	for i, value := range r {
		switch target := dest[i].(type) {
		case *int64:
			*target = value.(int64)
		case *int:
			*target = int(value.(int64))
		case *string:
			*target = value.(string)
		case *time.Time:
			*target = value.(time.Time)
		case *sql.NullInt64:
			*target = sql.NullInt64{Int64: value.(int64), Valid: true}
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	return nil
}

func TestScanLiveAnchorStyleUsesCardProfileOnlyInCardMode(t *testing.T) {
	row := func(mode string) styleModeTestRow {
		return styleModeTestRow{
			int64(7), int64(14), "主播", "介绍", "active",
			`{"summary":"样本风格"}`, mode, `{"summary":"风格卡"}`,
			int64(2), int64(1), int64(0), int64(8), int64(8),
			time.Now(), time.Now(), "female", "", int64(0),
		}
	}
	card, err := scanLiveAnchorStyle(row("card"))
	if err != nil || card.StyleSourceMode != "card" || card.Profile.Summary != "风格卡" || card.StyleCard == nil || card.StyleCard.Summary != "风格卡" {
		t.Fatalf("card mode not effective: %#v err=%v", card, err)
	}
	sample, err := scanLiveAnchorStyle(row("sample"))
	if err != nil || sample.StyleSourceMode != "sample" || sample.Profile.Summary != "样本风格" || sample.StyleCard == nil || sample.StyleCard.Summary != "风格卡" {
		t.Fatalf("sample mode not effective: %#v err=%v", sample, err)
	}
}
