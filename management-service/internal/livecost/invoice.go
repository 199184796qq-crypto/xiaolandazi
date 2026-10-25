package livecost

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Invoice struct {
	TenantID          int64  `json:"tenant_id"`
	RoomID            int64  `json:"room_id"`
	Provider          string `json:"provider"`
	ProviderRequestID string `json:"provider_request_id"`
	ActualMicros      int64  `json:"actual_micros"`
	Currency          string `json:"currency"`
}

func (i Invoice) Validate() error {
	if i.TenantID <= 0 || i.RoomID <= 0 || strings.TrimSpace(i.Provider) == "" || len(i.Provider) > 96 || strings.TrimSpace(i.ProviderRequestID) == "" || len(i.ProviderRequestID) > 192 || i.ActualMicros < 0 || i.Currency != "CNY" {
		return errors.New("账单须含正确房间、供应商请求号、人民币币种及非负微元金额")
	}
	return nil
}

// ParseInvoices accepts exported request-level invoices only. The parser never
// infers a payment from audio duration, playback time or a cached media archive.
func ParseInvoices(reader io.Reader, format string) ([]Invoice, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 10*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 10*1024*1024 {
		return nil, errors.New("账单文件超过10MB")
	}
	var invoices []Invoice
	switch strings.ToLower(format) {
	case "json":
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&invoices); err != nil {
			return nil, err
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return nil, errors.New("账单文件含多余内容")
		}
	case "csv":
		csvReader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
		csvReader.FieldsPerRecord = -1
		header, e := csvReader.Read()
		if e != nil {
			return nil, e
		}
		positions := map[string]int{}
		for index, name := range header {
			name = strings.TrimSpace(name)
			if _, exists := positions[name]; exists {
				return nil, errors.New("账单列名重复")
			}
			positions[name] = index
		}
		for _, name := range []string{"tenant_id", "room_id", "provider", "provider_request_id", "actual_micros", "currency"} {
			if _, ok := positions[name]; !ok {
				return nil, fmt.Errorf("账单缺列: %s", name)
			}
		}
		for line := 2; ; line++ {
			row, e := csvReader.Read()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if len(row) != len(header) {
				return nil, fmt.Errorf("账单第%d行列数不一致", line)
			}
			value := func(name string) string { return strings.TrimSpace(row[positions[name]]) }
			invoice := Invoice{Provider: value("provider"), ProviderRequestID: value("provider_request_id"), Currency: value("currency")}
			invoice.TenantID, e = strconv.ParseInt(value("tenant_id"), 10, 64)
			if e != nil {
				return nil, fmt.Errorf("账单第%d行租户编号无效", line)
			}
			invoice.RoomID, e = strconv.ParseInt(value("room_id"), 10, 64)
			if e != nil {
				return nil, fmt.Errorf("账单第%d行房间编号无效", line)
			}
			invoice.ActualMicros, e = strconv.ParseInt(value("actual_micros"), 10, 64)
			if e != nil {
				return nil, fmt.Errorf("账单第%d行微元金额无效", line)
			}
			invoices = append(invoices, invoice)
			if len(invoices) > 10000 {
				return nil, errors.New("每批账单最多10000条")
			}
		}
	default:
		return nil, errors.New("账单格式只支持json或csv")
	}
	if len(invoices) == 0 || len(invoices) > 10000 {
		return nil, errors.New("每批账单须为1至10000条")
	}
	seen := map[string]Invoice{}
	out := make([]Invoice, 0, len(invoices))
	for _, invoice := range invoices {
		if err = invoice.Validate(); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d\x00%d\x00%s\x00%s", invoice.TenantID, invoice.RoomID, invoice.Provider, invoice.ProviderRequestID)
		if old, exists := seen[key]; exists {
			if old != invoice {
				return nil, errors.New("同一供应商请求出现冲突账单")
			}
			continue
		}
		seen[key] = invoice
		out = append(out, invoice)
	}
	return out, nil
}
