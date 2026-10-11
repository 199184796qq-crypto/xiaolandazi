package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"livecompanion/management/internal/anchorstylescore"
)

type Segment struct {
	Index int `json:"index"`
	CandidateID string `json:"candidate_id"`
	Text string `json:"text"`
	Round int `json:"round"`
	Phase string `json:"phase"`
	Task string `json:"task"`
	Start float64 `json:"start"`
}

func main() {
	if len(os.Args)!=3 { panic("fixture and output paths required") }
	raw,err:=os.ReadFile(os.Args[1]);if err!=nil{panic(err)}
	var input struct{Session int `json:"session"`; Baseline json.RawMessage `json:"baseline"`; Segments []Segment `json:"segments"`}
	if err=json.Unmarshal(raw,&input);err!=nil{panic(err)}
	baseline,err:=anchorstylescore.Parse(string(input.Baseline));if err!=nil{panic(err)}
	all:=[]string{};tasks:=[]string{};rounds:=map[int][]string{};roundTasks:=map[int][]string{}
	segmentReports:=[]map[string]any{}
	for _,s:=range input.Segments {
		all=append(all,s.Text);tasks=append(tasks,s.Task);rounds[s.Round]=append(rounds[s.Round],s.Text);roundTasks[s.Round]=append(roundTasks[s.Round],s.Task)
		segmentReports=append(segmentReports,map[string]any{"segment":s,"score":anchorstylescore.Score(s.Text,baseline,s.Task)})
	}
	text:=strings.Join(all,"\n");normalized:=[]rune{}
	for _,r:=range text{if !unicode.IsSpace(r){normalized=append(normalized,r)}}
	windows:=[]map[string]any{}
	for i:=0;i<len(normalized);i+=1000 {
		end:=min(i+1000,len(normalized));part:=string(normalized[i:end])
		windows=append(windows,map[string]any{"index":i/1000+1,"chars":end-i,"complete_1000":end-i==1000,"score":anchorstylescore.Score(part,baseline,""),"text":part})
	}
	keys:=[]int{};for k:=range rounds{keys=append(keys,k)};sort.Ints(keys)
	roundReports:=[]map[string]any{}
	for _,k:=range keys{roundReports=append(roundReports,map[string]any{"round":k,"segments":len(rounds[k]),"score":anchorstylescore.Score(strings.Join(rounds[k],"\n"),baseline,strings.Join(roundTasks[k],"、"))})}
	output:=map[string]any{"session":input.Session,"segments":len(input.Segments),"scope":"actual played mainline only; excludes interaction and bridge","aggregate":anchorstylescore.Score(text,baseline,strings.Join(tasks,"、")),"rounds":roundReports,"windows":windows,"segment_reports":segmentReports,"baseline":json.RawMessage(input.Baseline)}
	data,err:=json.MarshalIndent(output,"","  ");if err!=nil{panic(err)}
	if err=os.WriteFile(os.Args[2],data,0600);err!=nil{panic(err)}
	fmt.Printf("session=%d segments=%d chars=%d windows=%d score=%d/100\n",input.Session,len(input.Segments),len(normalized),len(windows),anchorstylescore.Score(text,baseline,strings.Join(tasks,"、")).OverallScore)
}
