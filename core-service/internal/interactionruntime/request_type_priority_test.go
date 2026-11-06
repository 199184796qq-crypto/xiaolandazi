package interactionruntime

import (
	"testing"
	"time"
)

func TestRequestTypesKeepNormalQuestionScheduling(t *testing.T) {
	for _, label := range []string{"请求", "要求"} {
		t.Run(label, func(t *testing.T) {
			question := &Task{Work: Work{Kind: "semantic", At: base, Sequence: 1}, Content: Content{Intent: Intent{Relevant: true, Question: true, Type: "提问"}}}
			request := &Task{Work: Work{Kind: "semantic", At: base.Add(time.Second), Sequence: 2}, Content: Content{Intent: Intent{Relevant: true, Question: true, Type: label}}}
			complaint := &Task{Work: Work{Kind: "semantic", At: base.Add(-time.Second)}, Content: Content{Intent: Intent{Relevant: true, Question: true, Type: "投诉"}}}
			tasks := []*Task{question, request, complaint}
			sortReady(&room{}, tasks)
			if tasks[0] != complaint || tasks[1] != request || tasks[2] != question {
				t.Fatalf("%s changed the established complaint/normal/freshness ordering: priorities=%d,%d,%d", label, contentPriority(tasks[0]), contentPriority(tasks[1]), contentPriority(tasks[2]))
			}
			if contentPriority(request) != contentPriority(question) {
				t.Fatalf("%s must retain the same content priority as a normal question", label)
			}
		})
	}
}
