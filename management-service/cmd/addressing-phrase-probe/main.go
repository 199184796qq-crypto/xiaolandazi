// Command addressing-phrase-probe exercises the address-sentence step against
// real model output. It builds a few scenarios from production-shaped names,
// emits prompts, then validates saved answers.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"

	"livecompanion/management/internal/addressing"
)

type scenario struct {
	ID   string
	Desc string
	In   addressing.PhraseInput
}

func scenarios() []scenario {
	terms := []string{"老乡", "朋友"}
	labels := []string{"几位朋友", "刚来的几位"}
	person := func(base, short string) addressing.FormSet {
		return addressing.FormsFor(base, short, terms)
	}
	return []scenario{
		{ID: "1", Desc: "问候·单人 小斌", In: addressing.PhraseInput{Mode: "single", People: []addressing.FormSet{person("小斌", "小斌")}, Terms: terms, GroupLabels: labels}},
		{ID: "2", Desc: "问候·单人 梅梅", In: addressing.PhraseInput{Mode: "single", People: []addressing.FormSet{person("梅梅", "梅梅")}, Terms: terms, GroupLabels: labels}},
		{ID: "3", Desc: "问候·单人 樱桃姐", In: addressing.PhraseInput{Mode: "single", People: []addressing.FormSet{person("樱桃姐", "樱桃")}, Terms: terms, GroupLabels: labels}},
		{ID: "4", Desc: "问候·单人 挖坑不会埋", In: addressing.PhraseInput{Mode: "single", People: []addressing.FormSet{person("挖坑不会埋", "挖坑")}, Terms: terms, GroupLabels: labels}},
		{ID: "5", Desc: "问候·多人 小斌/梅梅", In: addressing.PhraseInput{Mode: "multi", People: []addressing.FormSet{person("小斌", "小斌"), person("梅梅", "梅梅")}, Terms: terms, GroupLabels: labels}},
		{ID: "6", Desc: "问候·多人 樱桃姐/张云锋", In: addressing.PhraseInput{Mode: "multi", People: []addressing.FormSet{person("樱桃姐", "樱桃"), person("张云锋", "张云")}, Terms: terms, GroupLabels: labels}},
		{ID: "7", Desc: "问候·多人 小陈/老王/阿芳", In: addressing.PhraseInput{Mode: "multi", People: []addressing.FormSet{person("小陈", "小陈"), person("老王", "老王"), person("阿芳", "阿芳")}, Terms: terms, GroupLabels: labels}},
		{ID: "8", Desc: "问候·多人 账号七二一/一零零", In: addressing.PhraseInput{Mode: "multi", People: []addressing.FormSet{person("用户七二一", "七二一"), person("用户一零零", "一零零")}, Terms: terms, GroupLabels: labels}},
		{ID: "9", Desc: "问候·全部不合规 只用集体", In: addressing.PhraseInput{Mode: "group", People: nil, Terms: terms, GroupLabels: labels}},
		{ID: "10", Desc: "问候·多人 余哥/芝士小宝贝", In: addressing.PhraseInput{Mode: "multi", People: []addressing.FormSet{person("余哥", "余哥"), person("芝士小宝贝", "芝士")}, Terms: terms, GroupLabels: labels}},
		{ID: "11", Desc: "问答·提问者 小斌", In: addressing.PhraseInput{Mode: "single", Scene: "answer", People: []addressing.FormSet{person("小斌", "小斌")}, Terms: terms, GroupLabels: labels}},
		{ID: "12", Desc: "问答·提问者 挖坑不会埋", In: addressing.PhraseInput{Mode: "single", Scene: "answer", People: []addressing.FormSet{person("挖坑不会埋", "挖坑")}, Terms: terms, GroupLabels: labels}},
	}
}

func main() {
	emit := flag.String("emit", "", "directory to write scenario prompts")
	rawDir := flag.String("raw", "", "directory holding scenario_N.json model answers")
	flag.Parse()
	r := rand.New(rand.NewSource(7))
	items := scenarios()
	switch {
	case *emit != "":
		if err := os.MkdirAll(*emit, 0o755); err != nil {
			panic(err)
		}
		for i, sc := range items {
			system, user := addressing.PhrasePrompt(sc.In, r)
			_ = os.WriteFile(fmt.Sprintf("%s/scenario_%d.system.txt", *emit, i+1), []byte(system), 0o644)
			_ = os.WriteFile(fmt.Sprintf("%s/scenario_%d.user.txt", *emit, i+1), []byte(user), 0o644)
			fmt.Printf("%s  %s\n", sc.ID, sc.Desc)
		}
	case *rawDir != "":
		for i, sc := range items {
			raw, err := os.ReadFile(fmt.Sprintf("%s/scenario_%d.json", *rawDir, i+1))
			if err != nil {
				fmt.Printf("%s %-40s READ_ERROR\n", sc.ID, sc.Desc)
				continue
			}
			address, err := addressing.ParsePhrase(string(raw), sc.In)
			if err != nil {
				fmt.Printf("%s %-40s FALLBACK %-16s | model=%s\n", sc.ID, sc.Desc, addressing.FallbackPhrase(sc.In), string(raw))
				continue
			}
			fmt.Printf("%s %-40s OK     %s\n", sc.ID, sc.Desc, address)
		}
	default:
		fmt.Println("use -emit DIR or -raw DIR")
	}
}
