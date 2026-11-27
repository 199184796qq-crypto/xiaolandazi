package httpapi

import "testing"

func TestParseMaterialClaimItemsLoose(t *testing.T) {
	cases := []string{
		"{\"items\":[{\"claim_key\":\"price\",\"content\":\"87.6元\",\"value\":87.6,\"conditions\":[]}]}",
		"```json\n{'claims':[{'key':'spec','content':'900g','conditions':'川渝地区',}]}\n```",
		"{items:[{key:price,content:'48.8元',conditions:[{field:'region',op:'in',values:['四川']}]}]}",
		"{\"items\":[{\"content\":\"包邮\",\"conditions\":\"除新疆外\"}",
	}
	for _, raw := range cases {
		items, err := parseMaterialClaimItems(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if len(items) != 1 || items[0].Content == "" {
			t.Fatalf("parse %q got %+v", raw, items)
		}
	}
}
