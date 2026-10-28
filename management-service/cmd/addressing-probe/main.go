// Command addressing-probe prints how the program-side nickname rules judge real
// names. It never calls a model, so it is safe to run against production data.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"livecompanion/management/internal/addressing"
)

func main() {
	rules := addressing.LoadDefault()
	if path := os.Getenv("NICKNAME_RULES_CONFIG"); path != "" {
		if loaded, err := addressing.Load(path); err == nil {
			rules = loaded
		}
	}
	fmt.Printf("rules version=%d dice.max=%d pick=%s\n", rules.Version, rules.Dice.Max, rules.Dice.Pick)
	fmt.Printf("%-3s %-28s %-10s %-14s %s\n", "#", "原昵称", "判定", "原因", "可播形式")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	index := 0
	var viewers []addressing.Viewer
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		userID, nickname := "", line
		if parts := strings.SplitN(line, "\t", 2); len(parts) == 2 {
			userID, nickname = parts[0], parts[1]
		}
		index++
		decision := rules.Check(nickname)
		fmt.Printf("%-3d %-28s %-10s %-14s %s\n", index, nickname, decision.Status, decision.Reason, decision.Text)
		viewers = append(viewers, addressing.Viewer{UserID: userID, RawName: nickname})
	}
	_ = viewers
}
