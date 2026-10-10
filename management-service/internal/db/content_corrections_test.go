package db

import (
	"context"
	"livecompanion/management/internal/contentpools"
	"testing"
)

func TestClarifyingCorrectionCannotCommit(t *testing.T) {
	c := contentpools.Correction{Content: contentpools.CorrectionContent{Question: "哪个商品？", Fact: "尚未采用的草稿"}}
	if _, e := (&Store{}).SaveContentCorrection(context.Background(), 1, 1, 1, contentpools.Set{}, contentpools.Set{}, c, PoolJob{}); e == nil {
		t.Fatal("unconfirmed correction reached database")
	}
}
