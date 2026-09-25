package roomintel

import (
	"sort"
	"strings"
	"time"
	"unicode"
)

type Heat string

const (
	HeatCold       Heat = "COLD"
	HeatWarm       Heat = "WARM"
	HeatBusy       Heat = "BUSY"
	HeatHot        Heat = "HOT"
	HeatOverheated Heat = "OVERHEATED"
)

const (
	realtimeQuestionDetailRetention = 10 * time.Hour
	ttsAggregateWindow              = 30 * time.Minute
)

type EventType string

const (
	EventEnter       EventType = "member"
	EventChat        EventType = "chat"
	EventLike        EventType = "like"
	EventFollow      EventType = "follow"
	EventOrder       EventType = "order"
	EventOrderSignal EventType = "order_signal"
	EventGift        EventType = "gift"
	EventRoom        EventType = "room"
)

type Event struct {
	EventID    int64
	Type       EventType
	UserID     string
	Nickname   string
	Topic      string
	Content    string
	Question   bool
	Negative   bool
	Count      int64
	Online     int
	OccurredAt time.Time
}

type TopicQuestion struct {
	EventID    int64
	UserID     string
	Nickname   string
	Content    string
	OccurredAt time.Time
}

type TopicBucket struct {
	Topic                 string
	Count                 int
	UniqueUsers           int
	LastSeenAt            time.Time
	LastAnsweredAt        time.Time
	SampleQuestions       []string
	Questions             []TopicQuestion
	TTSQuestions          []TopicQuestion
	TTSEligibleCount      int
	ArchivedQuestionCount int
}

type Snapshot struct {
	Heat                Heat
	OnlineCount         int
	Entries30s          int
	Entries60s          int
	Chats30s            int
	Likes30s            int64
	Follows30s          int
	Orders30s           int
	OrderSignals30s     int
	OrderSignals60s     int
	OrderSignalUsers60s int
	OrderSignalSamples  []string
	SessionEntries      int
	SessionChats        int
	SessionLikes        int64
	SessionFollows      int
	SessionGifts        int
	UniqueChatters30s   int
	QuestionCount30s    int
	NegativeFeedback30s int
	QuestionPressure    float64
	AudienceTurnover5m  float64
	TopTopics           []TopicBucket
	PreferAggregateQNA  bool
	PreferOneToOneQNA   bool
}

type Pool struct {
	events  []Event
	buckets map[string]*bucketState
	aliases map[string]string
	online  int

	sessionEntries int
	sessionChats   int
	sessionLikes   int64
	sessionFollows int
	sessionGifts   int
}

type bucketState struct {
	topic          string
	seed           string
	events         []Event
	archivedCount  int
	lastAnsweredAt time.Time
}

func NewPool() *Pool {
	return &Pool{
		buckets: map[string]*bucketState{},
		aliases: map[string]string{},
	}
}

func (p *Pool) Add(event Event) {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.Count <= 0 {
		event.Count = 1
	}
	p.events = append(p.events, event)
	if event.Online > 0 {
		p.online = event.Online
	}
	switch event.Type {
	case EventEnter:
		p.sessionEntries += int(event.Count)
	case EventChat:
		p.sessionChats += int(event.Count)
	case EventLike:
		p.sessionLikes += event.Count
	case EventFollow:
		p.sessionFollows += int(event.Count)
	case EventGift:
		p.sessionGifts += int(event.Count)
	}
	if event.Type == EventChat && event.Question {
		topic, seed := p.questionTopic(event.Content)
		if topic == "" {
			topic = strings.TrimSpace(strings.ToUpper(event.Topic))
			seed = normalizeQuestionText(event.Content)
		}
		if topic != "" {
			topic = p.resolveTopic(topic)
			event.Topic = topic
			state := p.buckets[topic]
			if state == nil {
				state = &bucketState{topic: topic, seed: seed}
				p.buckets[topic] = state
			}
			state.events = append(state.events, event)
		}
	}
}

func (p *Pool) RemoveUser(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	kept := p.events[:0]
	for _, event := range p.events {
		if strings.EqualFold(strings.TrimSpace(event.UserID), userID) {
			continue
		}
		kept = append(kept, event)
	}
	p.events = kept
	for key, state := range p.buckets {
		bucketEvents := state.events[:0]
		for _, event := range state.events {
			if strings.EqualFold(strings.TrimSpace(event.UserID), userID) {
				continue
			}
			bucketEvents = append(bucketEvents, event)
		}
		state.events = bucketEvents
		if len(state.events) == 0 {
			delete(p.buckets, key)
		}
	}
	p.pruneAliases()
}

func (p *Pool) MarkAnswered(topic string, at time.Time) {
	topic = p.resolveTopic(strings.TrimSpace(topic))
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if state := p.buckets[topic]; state != nil {
		state.lastAnsweredAt = at
	}
}

func (p *Pool) MergeTopics(representative string, sources []string) int {
	if p == nil {
		return 0
	}
	representative = p.resolveTopic(strings.TrimSpace(representative))
	target := p.buckets[representative]
	if representative == "" || target == nil {
		return 0
	}
	if p.aliases == nil {
		p.aliases = map[string]string{}
	}
	merged := 0
	seen := map[string]struct{}{}
	for _, item := range sources {
		rawSource := strings.TrimSpace(item)
		if rawSource == "" {
			continue
		}
		source := p.resolveTopic(rawSource)
		if source == "" || source == representative {
			p.aliases[rawSource] = representative
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		state := p.buckets[source]
		if state == nil {
			continue
		}
		target.events = append(target.events, state.events...)
		if state.lastAnsweredAt.After(target.lastAnsweredAt) {
			target.lastAnsweredAt = state.lastAnsweredAt
		}
		if target.seed == "" {
			target.seed = state.seed
		}
		delete(p.buckets, source)
		p.aliases[rawSource] = representative
		p.aliases[source] = representative
		for alias, destination := range p.aliases {
			if destination == source {
				p.aliases[alias] = representative
			}
		}
		merged++
	}
	if merged > 0 {
		sort.SliceStable(target.events, func(i, j int) bool {
			if target.events[i].OccurredAt.Equal(target.events[j].OccurredAt) {
				return target.events[i].EventID < target.events[j].EventID
			}
			return target.events[i].OccurredAt.Before(target.events[j].OccurredAt)
		})
	}
	return merged
}

func (p *Pool) resolveTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" || len(p.aliases) == 0 {
		return topic
	}
	seen := map[string]struct{}{}
	for step := 0; step < 32; step++ {
		next := strings.TrimSpace(p.aliases[topic])
		if next == "" || next == topic {
			return topic
		}
		if _, ok := seen[next]; ok {
			return topic
		}
		seen[topic] = struct{}{}
		topic = next
	}
	return topic
}

func (p *Pool) pruneAliases() {
	if len(p.aliases) == 0 {
		return
	}
	for alias, destination := range p.aliases {
		destination = p.resolveTopic(destination)
		if _, ok := p.buckets[destination]; !ok {
			delete(p.aliases, alias)
			continue
		}
		p.aliases[alias] = destination
	}
}

func (p *Pool) Snapshot(now time.Time, answerCapacityPerMinute float64) Snapshot {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if answerCapacityPerMinute <= 0 {
		answerCapacityPerMinute = 6
	}
	cut30 := now.Add(-30 * time.Second)
	cut60 := now.Add(-60 * time.Second)
	cut5m := now.Add(-5 * time.Minute)

	uniqueChatters := map[string]struct{}{}
	orderSignalUsers := map[string]struct{}{}
	entries5m := 0
	out := Snapshot{
		OnlineCount:    p.online,
		SessionEntries: p.sessionEntries,
		SessionChats:   p.sessionChats,
		SessionLikes:   p.sessionLikes,
		SessionFollows: p.sessionFollows,
		SessionGifts:   p.sessionGifts,
	}

	kept := p.events[:0]
	for _, event := range p.events {
		if event.OccurredAt.Before(now.Add(-15 * time.Minute)) {
			continue
		}
		kept = append(kept, event)
		if !event.OccurredAt.Before(cut30) {
			switch event.Type {
			case EventEnter:
				out.Entries30s += int(event.Count)
			case EventChat:
				out.Chats30s += int(event.Count)
				if event.Question {
					out.QuestionCount30s += int(event.Count)
				}
				if event.Negative {
					out.NegativeFeedback30s += int(event.Count)
				}
				if event.UserID != "" {
					uniqueChatters[event.UserID] = struct{}{}
				}
			case EventLike:
				out.Likes30s += event.Count
			case EventFollow:
				out.Follows30s += int(event.Count)
			case EventOrder:
				out.Orders30s += int(event.Count)
			case EventOrderSignal:
				out.OrderSignals30s += int(event.Count)
			}
		}
		if !event.OccurredAt.Before(cut60) {
			switch event.Type {
			case EventEnter:
				out.Entries60s += int(event.Count)
			case EventOrderSignal:
				out.OrderSignals60s += int(event.Count)
				if strings.TrimSpace(event.UserID) != "" {
					orderSignalUsers[event.UserID] = struct{}{}
				}
				content := strings.TrimSpace(event.Content)
				if content != "" && len(out.OrderSignalSamples) < 3 {
					seen := false
					for _, sample := range out.OrderSignalSamples {
						if sample == content {
							seen = true
							break
						}
					}
					if !seen {
						out.OrderSignalSamples = append(out.OrderSignalSamples, content)
					}
				}
			}
		}
		if !event.OccurredAt.Before(cut5m) && event.Type == EventEnter {
			entries5m += int(event.Count)
		}
	}
	p.events = kept
	out.UniqueChatters30s = len(uniqueChatters)
	out.OrderSignalUsers60s = len(orderSignalUsers)
	out.QuestionPressure = float64(out.QuestionCount30s*2) / answerCapacityPerMinute
	if out.OnlineCount > 0 {
		out.AudienceTurnover5m = float64(entries5m) / float64(out.OnlineCount)
	}

	questionDetailCutoff := now.Add(-realtimeQuestionDetailRetention)
	ttsCutoff := now.Add(-ttsAggregateWindow)
	for topic, state := range p.buckets {
		users := map[string]struct{}{}
		retained := state.events[:0]
		for _, event := range state.events {
			if event.OccurredAt.Before(questionDetailCutoff) {
				state.archivedCount += int(event.Count)
				continue
			}
			retained = append(retained, event)
		}
		state.events = retained
		if len(state.events) == 0 {
			delete(p.buckets, topic)
			for alias, target := range p.aliases {
				if alias == topic || target == topic {
					delete(p.aliases, alias)
				}
			}
			continue
		}

		bucket := TopicBucket{
			Topic:                 state.topic,
			LastAnsweredAt:        state.lastAnsweredAt,
			Count:                 state.archivedCount,
			ArchivedQuestionCount: state.archivedCount,
		}
		for _, event := range state.events {
			bucket.Count += int(event.Count)
			if event.UserID != "" {
				users[event.UserID] = struct{}{}
			}
			if event.OccurredAt.After(bucket.LastSeenAt) {
				bucket.LastSeenAt = event.OccurredAt
			}
			content := strings.TrimSpace(event.Content)
			if content != "" && len(bucket.SampleQuestions) < 3 {
				seen := false
				for _, sample := range bucket.SampleQuestions {
					if sample == content {
						seen = true
						break
					}
				}
				if !seen {
					bucket.SampleQuestions = append(bucket.SampleQuestions, content)
				}
			}
		}
		bucket.UniqueUsers = len(users)
		const maxQuestionDetails = 50
		start := 0
		if len(state.events) > maxQuestionDetails {
			start = len(state.events) - maxQuestionDetails
		}
		for index := len(state.events) - 1; index >= start; index-- {
			event := state.events[index]
			question := TopicQuestion{
				EventID:    event.EventID,
				UserID:     event.UserID,
				Nickname:   event.Nickname,
				Content:    strings.TrimSpace(event.Content),
				OccurredAt: event.OccurredAt,
			}
			bucket.Questions = append(bucket.Questions, question)
			if !event.OccurredAt.Before(ttsCutoff) {
				bucket.TTSQuestions = append(bucket.TTSQuestions, question)
				bucket.TTSEligibleCount += int(event.Count)
			}
		}
		out.TopTopics = append(out.TopTopics, bucket)
	}
	sort.Slice(out.TopTopics, func(i, j int) bool {
		if out.TopTopics[i].Count == out.TopTopics[j].Count {
			return out.TopTopics[i].LastSeenAt.After(out.TopTopics[j].LastSeenAt)
		}
		return out.TopTopics[i].Count > out.TopTopics[j].Count
	})
	if len(out.TopTopics) > 20 {
		out.TopTopics = out.TopTopics[:20]
	}

	out.Heat = classify(out)
	out.PreferAggregateQNA = out.Heat == HeatHot || out.Heat == HeatOverheated || out.QuestionPressure >= 1.5
	out.PreferOneToOneQNA = (out.Heat == HeatCold || out.Heat == HeatWarm) && out.QuestionPressure < 0.8
	return out
}

const questionSimilarityThreshold = 0.56

func QuestionTopic(text string) string {
	if topic, _, ok := coarseQuestionFamily(text); ok {
		return topic
	}
	seed := normalizeQuestionText(text)
	if seed == "" {
		return ""
	}
	return "Q:" + seed
}

func (p *Pool) questionTopic(text string) (string, string) {
	if topic, seed, ok := coarseQuestionFamily(text); ok {
		return topic, seed
	}
	seed := normalizeQuestionText(text)
	if seed == "" {
		return "", ""
	}
	exactTopic := "Q:" + seed
	if canonical := p.resolveTopic(exactTopic); canonical != exactTopic {
		return canonical, seed
	}
	bestTopic := ""
	bestScore := 0.0
	for topic, state := range p.buckets {
		candidate := state.seed
		if candidate == "" && len(state.events) > 0 {
			candidate = normalizeQuestionText(state.events[0].Content)
		}
		score := questionTextSimilarity(seed, candidate)
		if score > bestScore {
			bestScore = score
			bestTopic = topic
		}
	}
	if bestTopic != "" && bestScore >= questionSimilarityThreshold {
		return bestTopic, seed
	}
	return exactTopic, seed
}

func coarseQuestionFamily(text string) (string, string, bool) {
	value := strings.ToLower(strings.TrimSpace(text))
	if value == "" {
		return "", "", false
	}
	containsAny := func(parts ...string) bool {
		for _, part := range parts {
			if strings.Contains(value, part) {
				return true
			}
		}
		return false
	}
	family := func(label string) (string, string, bool) {
		return "FAMILY:" + label, label, true
	}

	// Explicit price/cost wording wins first, including shipping fees.
	if containsAny("多少钱", "价格", "价钱", "什么价", "几块", "几元", "优惠", "折扣", "便宜", "贵不贵", "贵吗", "运费多少", "邮费多少", "运费多少钱", "邮费多少钱") {
		return family("价格费用")
	}
	// Buying intent must win over generic location wording such as “哪里买”.
	if containsAny("怎么买", "如何买", "怎么购买", "如何购买", "怎么拍", "拍哪个", "哪个链接", "购买链接", "下单链接", "在哪里买", "在哪买", "哪里买", "怎么下单", "如何下单") {
		return family("购买下单")
	}
	if containsAny("退款", "退货", "售后", "破损", "坏了", "漏气", "少发", "漏发", "错发", "补发", "退换") {
		return family("售后退款")
	}
	if containsAny("发货", "快递", "物流", "多久到", "几天到", "什么时候到", "何时到", "包邮", "邮费", "运费", "送货上楼", "送上楼", "上楼吗") {
		return family("发货物流")
	}
	if containsAny("规格", "重量", "几斤", "多少斤", "多少克", "几克", "多少个", "几个装", "几只", "几袋", "几盒", "尺寸", "多大") {
		return family("规格重量")
	}
	if containsAny("怎么吃", "如何吃", "怎么做", "如何做", "怎么加热", "如何加热", "空气炸锅", "微波炉", "蒸多久", "煮多久", "炸多久") {
		return family("吃法加热")
	}
	if containsAny("产地", "哪里产", "哪产的", "哪生产", "哪里生产", "哪里的货", "什么地方产") {
		return family("产地来源")
	}
	if containsAny("好吃吗", "好不好吃", "口感", "味道", "新鲜", "正宗", "品质", "质量", "辣不辣", "咸不咸", "甜不甜") {
		return family("品质口感")
	}
	if containsAny("保质期", "保存", "冷藏", "冷冻", "能放多久", "放多久", "怎么存", "如何存") {
		return family("保存保质")
	}
	return "", "", false
}

func normalizeQuestionText(text string) string {
	value := strings.ToLower(strings.TrimSpace(text))
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"请问", "", "麻烦问下", "", "麻烦问一下", "", "想问一下", "", "问一下", "",
		"在哪个地方", "地点", "什么地方", "地点", "在哪里", "地点", "在哪儿", "地点",
		"地址在哪里", "地点", "地址在哪", "地点", "位置在哪", "地点", "门店在哪", "地点", "店在哪", "地点",
		"多少钱", "价格", "价格多少", "价格", "价钱多少", "价格", "什么价格", "价格", "什么价", "价格",
		"怎么购买", "购买方式", "如何购买", "购买方式", "怎么买", "购买方式", "怎么下单", "购买方式", "怎么拍", "购买方式",
	)
	value = replacer.Replace(value)
	// Keep clustering dynamic, but normalize close phrasings of the same
	// real question so later variants can merge into the first visible bucket.
	// The UI label still comes from the first raw question, not this seed.
	shippingPhrases := []string{
		"从哪里发货", "从哪儿发货", "从哪发货", "发货地哪里",
		"什么时候发货", "啥时候发货", "何时发货", "多久发货", "几天发货",
		"哪里发货", "哪儿发货",
	}
	for _, phrase := range shippingPhrases {
		value = strings.ReplaceAll(value, phrase, "发货")
	}
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	value = b.String()
	for _, suffix := range []string{"呢", "吗", "呀", "啊", "嘛"} {
		value = strings.TrimSuffix(value, suffix)
	}
	return value
}

func questionTextSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	ar, br := []rune(a), []rune(b)
	if len(ar) == 1 || len(br) == 1 {
		return 0
	}
	grams := func(value []rune) map[string]int {
		out := map[string]int{}
		for i := 0; i < len(value)-1; i++ {
			out[string(value[i:i+2])]++
		}
		return out
	}
	aGrams, bGrams := grams(ar), grams(br)
	common, totalA, totalB := 0, 0, 0
	for key, count := range aGrams {
		totalA += count
		if other := bGrams[key]; other > 0 {
			if count < other {
				common += count
			} else {
				common += other
			}
		}
	}
	for _, count := range bGrams {
		totalB += count
	}
	if totalA+totalB == 0 {
		return 0
	}
	return float64(2*common) / float64(totalA+totalB)
}

func classify(s Snapshot) Heat {
	if s.QuestionPressure >= 3 || s.Chats30s >= 40 || s.Entries30s >= 120 {
		return HeatOverheated
	}
	if s.QuestionPressure >= 1.5 || s.Chats30s >= 20 || s.Entries30s >= 60 {
		return HeatHot
	}
	if s.Chats30s >= 8 || s.Entries30s >= 20 || s.Follows30s >= 5 {
		return HeatBusy
	}
	if s.Chats30s >= 2 || s.Entries30s >= 5 {
		return HeatWarm
	}
	return HeatCold
}
