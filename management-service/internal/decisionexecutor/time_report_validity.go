package decisionexecutor

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reportClock = regexp.MustCompile(`([0-9零〇一二两三四五六七八九十]{1,3})(?:[:：]([0-9]{1,2})|[点时](?:(半|一刻|三刻)|([0-9零〇一二两三四五六七八九十]{1,3})分?)?)`)

func reportNumber(s string) (int, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	value, tens := 0, false
	for _, c := range s {
		if c == '十' {
			if tens {
				return 0, false
			}
			if value == 0 {
				value = 1
			}
			value *= 10
			tens = true
			continue
		}
		d, ok := digits[c]
		if !ok {
			return 0, false
		}
		if tens {
			value += d
		} else {
			value = value*10 + d
		}
	}
	return value, s != ""
}

// Validity is anchored to the original generation clock, never a retry clock.
// Explicit clocks must describe that clock; uncertain/contradictory statements
// are replaced by a current-period fallback, without another paid model call.
func timeReportValidity(text string, generated time.Time) (time.Time, bool) {
	now := generated.In(beijingZone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, beijingZone)
	until := day.AddDate(0, 0, 1)
	matchedPart := false
	periods := []struct {
		word     string
		from, to int
	}{
		{"凌晨", 0, 6}, {"早上", 6, 11}, {"早晨", 6, 11}, {"上午", 6, 12},
		{"中午", 11, 13}, {"下午", 13, 18}, {"午后", 13, 18}, {"傍晚", 17, 19}, {"晚上", 18, 24},
	}
	for _, part := range periods {
		if !strings.Contains(text, part.word) {
			continue
		}
		if now.Hour() < part.from || now.Hour() >= part.to {
			return time.Time{}, false
		}
		end := day.Add(time.Duration(part.to) * time.Hour)
		if end.Before(until) {
			until = end
		}
		matchedPart = true
	}
	if strings.Contains(text, "夜里") || strings.Contains(text, "深夜") {
		var nightEnd time.Time
		if now.Hour() >= 22 {
			nightEnd = day.AddDate(0, 0, 1).Add(6 * time.Hour)
		} else if now.Hour() < 6 {
			nightEnd = day.Add(6 * time.Hour)
		} else {
			return time.Time{}, false
		}
		if !matchedPart || nightEnd.Before(until) {
			until = nightEnd
		}
		matchedPart = true
	}
	if !matchedPart {
		endHour := 24
		switch h := now.Hour(); {
		case h < 6:
			endHour = 6
		case h < 11:
			endHour = 11
		case h < 13:
			endHour = 13
		case h < 18:
			endHour = 18
		}
		until = day.Add(time.Duration(endHour) * time.Hour)
	}
	// Common quantity/adverb expressions contain “一点” without a clock.
	clockText := text
	for _, phrase := range []string{"慢一点", "快一点", "早一点", "晚一点", "多一点", "少一点", "看一点", "听一点", "吃一点", "喝一点", "一点时间", "一点东西", "一点心意", "一点也", "一点都", "一点不"} {
		clockText = strings.ReplaceAll(clockText, phrase, "")
	}
	for _, match := range reportClock.FindAllStringSubmatch(clockText, -1) {
		hour, ok := reportNumber(match[1])
		if !ok || hour > 23 {
			return time.Time{}, false
		}
		// Spoken 12-hour clocks without a daypart follow the generation clock.
		if match[2] == "" && hour <= 12 && hour%12 == now.Hour()%12 {
			hour = now.Hour()
		}
		if hour != now.Hour() {
			return time.Time{}, false
		}
		end := day.Add(time.Duration(hour+1) * time.Hour)
		minuteText := match[2]
		if minuteText == "" {
			minuteText = match[4]
		}
		minute, precise := 0, minuteText != "" || match[3] != ""
		if minuteText != "" {
			var valid bool
			minute, valid = reportNumber(minuteText)
			if !valid || minute > 59 {
				return time.Time{}, false
			}
		}
		switch match[3] {
		case "半":
			minute = 30
		case "一刻":
			minute = 15
		case "三刻":
			minute = 45
		}
		if precise {
			if minute != now.Minute() {
				return time.Time{}, false
			}
			end = now.Truncate(time.Minute).Add(time.Minute)
		}
		if end.Before(until) {
			until = end
		}
	}
	for _, relative := range []string{"再过", "还有", "过一会", "马上", "快到", "刚过", "分钟后", "小时后"} {
		if strings.Contains(text, relative) {
			end := now.Truncate(time.Minute).Add(time.Minute)
			if end.Before(until) {
				until = end
			}
			break
		}
	}
	if strings.Contains(text, "今天") || strings.Contains(text, "今晚") {
		if end := day.AddDate(0, 0, 1); end.Before(until) {
			until = end
		}
	}
	return until.UTC(), !strings.Contains(text, "明天") && !strings.Contains(text, "昨天") && now.Before(until)
}
