package mainlineasset

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

type Role string
type Pool string
type Quality string
type PointGrade string

const (
	RoleMainline     Role = "MAINLINE"
	RoleInteraction  Role = "INTERACTION"
	RoleBridge       Role = "BRIDGE"
	RoleConversion   Role = "CONVERSION"
	RoleWarmup       Role = "WARMUP"
	RoleHumanization Role = "HUMANIZATION"

	PoolEvergreen    Pool = "EVERGREEN"
	PoolSession      Pool = "SESSION"
	PoolDynamic      Pool = "DYNAMIC"
	PoolHumanization Pool = "HUMANIZATION"

	QualityGold         Quality = "GOLD"
	QualitySilver       Quality = "SILVER"
	QualityExperimental Quality = "EXPERIMENTAL"
	QualityRetired      Quality = "RETIRED"

	GradeA PointGrade = "A"
	GradeB PointGrade = "B"
	GradeC PointGrade = "C"
)

type AlignmentToken struct {
	Text    string
	StartMS int64
	EndMS   int64
}

type SafePoint struct {
	ID       string
	AtMS     int64
	Score    int
	Grade    PointGrade
	Reason   string
	CanExit  bool
	CanEnter bool
}

type SemanticUnit struct {
	ID               string
	Topic            string
	SemanticGoal     string
	FactIDs          []string
	Text             string
	StartMS          int64
	EndMS            int64
	IndependentEntry bool
	ExitAllowed      bool
	SafeEntryPoints  []SafePoint
	SafeExitPoints   []SafePoint
	StrategyTags     []string
	NextUnitIDs      []string
}

type StrategyNode struct {
	ID       string
	Kind     string
	UnitID   string
	AtMS     int64
	Priority int
	Tags     []string
}

type CacheIdentity struct {
	Text           string
	VoiceID        string
	TTSProvider    string
	TTSModel       string
	StyleVersion   string
	ProsodyVersion string
	Speed          string
}

func (c CacheIdentity) Key() string {
	parts := []string{
		normalize(c.Text),
		strings.TrimSpace(c.VoiceID),
		strings.TrimSpace(c.TTSProvider),
		strings.TrimSpace(c.TTSModel),
		strings.TrimSpace(c.StyleVersion),
		strings.TrimSpace(c.ProsodyVersion),
		strings.TrimSpace(c.Speed),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

type Asset struct {
	ID           string
	Role         Role
	Pool         Pool
	Quality      Quality
	Label        string
	AudioURI     string
	DurationMS   int64
	FinalText    string
	Units        []SemanticUnit
	Alignment    []AlignmentToken
	Nodes        []StrategyNode
	CacheKey     string
	VoiceID      string
	TTSProvider  string
	TTSModel     string
	StyleVersion string
	CreatedAt    time.Time
	LastPlayedAt time.Time
	PlayCount    int64
}

func (a Asset) ValidateSchedulable() error {
	if strings.TrimSpace(a.ID) == "" {
		return errors.New("asset id is required")
	}
	if a.DurationMS <= 0 {
		return errors.New("asset duration must be positive")
	}
	if strings.TrimSpace(a.AudioURI) == "" {
		return errors.New("audio uri is required")
	}
	if a.Role == RoleMainline || a.Role == RoleConversion || a.Role == RoleWarmup {
		if len(a.Units) == 0 {
			return errors.New("schedulable mainline asset requires semantic units")
		}
		for _, unit := range a.Units {
			if strings.TrimSpace(unit.ID) == "" || strings.TrimSpace(unit.Text) == "" {
				return errors.New("semantic unit id/text is required")
			}
			if unit.EndMS <= unit.StartMS {
				return errors.New("semantic unit must have positive duration")
			}
			if unit.StartMS < 0 || unit.EndMS > a.DurationMS {
				return errors.New("semantic unit is outside asset duration")
			}
		}
	}
	return nil
}

func (a Asset) BestExitAfter(atMS int64, minGrade PointGrade) (SafePoint, bool) {
	points := make([]SafePoint, 0)
	for _, unit := range a.Units {
		for _, point := range unit.SafeExitPoints {
			if !point.CanExit || point.AtMS < atMS || !gradeAllowed(point.Grade, minGrade) {
				continue
			}
			points = append(points, point)
		}
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].AtMS == points[j].AtMS {
			return points[i].Score > points[j].Score
		}
		return points[i].AtMS < points[j].AtMS
	})
	if len(points) == 0 {
		return SafePoint{}, false
	}
	return points[0], true
}

func (a Asset) EntryForNode(nodeID string) (SafePoint, bool) {
	for _, node := range a.Nodes {
		if node.ID != nodeID {
			continue
		}
		for _, unit := range a.Units {
			if unit.ID != node.UnitID {
				continue
			}
			for _, point := range unit.SafeEntryPoints {
				if point.CanEnter {
					return point, true
				}
			}
		}
	}
	return SafePoint{}, false
}

func gradeAllowed(actual, minimum PointGrade) bool {
	rank := map[PointGrade]int{GradeA: 3, GradeB: 2, GradeC: 1}
	return rank[actual] >= rank[minimum]
}

func normalize(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
