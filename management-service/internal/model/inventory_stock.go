package model

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type InventoryStockProduct struct {
	ProductID int64  `json:"product_id"`
	Name      string `json:"name"`
	SKUCode   string `json:"sku_code"`
	Total     int64  `json:"total"`
	Available int64  `json:"available"`
	Transit   int64  `json:"transit"`
	Repair    int64  `json:"repair"`
	Scrap     int64  `json:"scrap"`
	Delivered int64  `json:"delivered"`
}

// Old callers may still send a combined batch_no. New callers send two parts.
func ComposeInventoryBatch(prefix, suffix, legacy string, now time.Time) (string, error) {
	prefix, suffix, legacy = strings.TrimSpace(prefix), strings.TrimSpace(suffix), strings.TrimSpace(legacy)
	batch := legacy
	if prefix != "" || suffix != "" || batch == "" {
		if prefix == "" {
			prefix = now.In(time.FixedZone("CST", 8*3600)).Format("20060102")
		}
		batch = prefix
		if suffix != "" {
			batch += "-" + suffix
		}
	}
	if utf8.RuneCountInString(batch) > 96 {
		return "", errors.New("组合批次不能超过 96 个字")
	}
	for _, r := range batch {
		if r < 32 || r == 127 {
			return "", errors.New("批次不能包含换行或控制字符")
		}
	}
	return batch, nil
}
