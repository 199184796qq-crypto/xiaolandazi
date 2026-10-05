// Package numericrepair implements the trusted runtime half of the
// numeric-repair example plugin. The language model never chooses the correct
// or deliberately wrong value: both are verified/constructed here.
package numericrepair

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"livecompanion/management/pkg/styleplugin"
)

const ExecutorName = "numeric_repair_v1"

type Executor struct{}

func (Executor) Compile(_ context.Context, manifest styleplugin.Manifest, instance styleplugin.Instance, input styleplugin.CompileContext) (styleplugin.CompiledFragment, error) {
	parameters, err := styleplugin.EffectiveParameters(manifest, instance)
	if err != nil {
		return styleplugin.CompiledFragment{}, err
	}
	if input.Serious {
		return styleplugin.CompiledFragment{}, errors.New("numeric repair is disabled in serious scenes")
	}
	return styleplugin.CompiledFragment{
		Instruction:  "只可对经计算器验证的派生算术结果制造一次轻微口算偏差，并在同一不可打断单元内立即纠正；不得改动输入事实。",
		MicroActions: []string{"state_information", "self_correction", "conclusion"},
		Parameters:   parameters,
	}, nil
}

// Calculation contains only integer minor units so floating-point rounding can
// never create or hide a fact change. Scale is the number of minor units in one
// displayed unit: 10 for one decimal place, 100 for cents, and so on.
type Calculation struct {
	Operation    string `json:"operation"`
	LeftMinor    int64  `json:"left_minor"`
	Right        int64  `json:"right"`
	CorrectMinor int64  `json:"correct_minor"`
	Scale        int64  `json:"scale"`
	Unit         string `json:"unit,omitempty"`
}

type AtomicSpeechUnit struct {
	WrongMinor     int64  `json:"wrong_minor"`
	CorrectMinor   int64  `json:"correct_minor"`
	Text           string `json:"text"`
	Interruptible  bool   `json:"interruptible"`
	AtomicAudio    bool   `json:"atomic_audio"`
	AtomicSubtitle bool   `json:"atomic_subtitle"`
}

// BuildAtomicUnit verifies the correct result, constructs a nearby wrong
// derived result deterministically, and returns the error+repair as one unit.
func BuildAtomicUnit(input Calculation, repairPhrase string) (AtomicSpeechUnit, error) {
	if input.Scale <= 0 || input.Scale > 1_000_000 {
		return AtomicSpeechUnit{}, errors.New("scale must be between 1 and 1000000")
	}
	for scale := input.Scale; scale > 1; scale /= 10 {
		if scale%10 != 0 {
			return AtomicSpeechUnit{}, errors.New("scale must be a power of ten")
		}
	}
	if input.LeftMinor < 0 || input.Right < 0 || input.CorrectMinor < 0 {
		return AtomicSpeechUnit{}, errors.New("numeric repair accepts non-negative commerce arithmetic only")
	}
	calculated, err := calculate(input.Operation, input.LeftMinor, input.Right)
	if err != nil {
		return AtomicSpeechUnit{}, err
	}
	if calculated != input.CorrectMinor {
		return AtomicSpeechUnit{}, errors.New("provided correct value does not match verified arithmetic")
	}
	repairPhrase = strings.TrimSpace(repairPhrase)
	if repairPhrase == "" {
		repairPhrase = "不对，我重新算一下"
	}
	step := input.Scale
	wrong := input.CorrectMinor + step
	if input.CorrectMinor > math.MaxInt64-step {
		wrong = input.CorrectMinor - step
	}
	if wrong == input.CorrectMinor {
		return AtomicSpeechUnit{}, errors.New("cannot construct a bounded derived-value variation")
	}
	unit := strings.TrimSpace(input.Unit)
	text := fmt.Sprintf("%s%s，%s，应该是%s%s", formatMinor(wrong, input.Scale), unit, repairPhrase, formatMinor(input.CorrectMinor, input.Scale), unit)
	return AtomicSpeechUnit{
		WrongMinor: wrong, CorrectMinor: input.CorrectMinor, Text: text,
		Interruptible: false, AtomicAudio: true, AtomicSubtitle: true,
	}, nil
}

func calculate(operation string, leftMinor, right int64) (int64, error) {
	switch operation {
	case "multiply":
		product := new(big.Int).Mul(big.NewInt(leftMinor), big.NewInt(right))
		if !product.IsInt64() {
			return 0, errors.New("calculation overflow")
		}
		return product.Int64(), nil
	case "add":
		if right > 0 && leftMinor > math.MaxInt64-right || right < 0 && leftMinor < math.MinInt64-right {
			return 0, errors.New("calculation overflow")
		}
		return leftMinor + right, nil
	case "subtract":
		if right == math.MinInt64 {
			return 0, errors.New("calculation overflow")
		}
		return calculate("add", leftMinor, -right)
	default:
		return 0, fmt.Errorf("unsupported operation %q", operation)
	}
}

func formatMinor(value, scale int64) string {
	if scale == 1 {
		return strconv.FormatInt(value, 10)
	}
	negative := value < 0
	if negative {
		value = -value
	}
	whole := value / scale
	fraction := strconv.FormatInt(value%scale+scale, 10)[1:]
	fraction = strings.TrimRight(fraction, "0")
	if fraction == "" {
		fraction = "0"
	}
	result := strconv.FormatInt(whole, 10) + "." + fraction
	if negative {
		return "-" + result
	}
	return result
}
