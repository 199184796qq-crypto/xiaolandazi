package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptchain"
)

type scriptGenerationMode string

const (
	scriptGenerationDefault   scriptGenerationMode = ""
	scriptGenerationRewrite   scriptGenerationMode = "rewrite"
	scriptGenerationPolish    scriptGenerationMode = "polish"
	scriptGenerationSpecified scriptGenerationMode = "specified"
)

var (
	errUnknownScriptGenerationMode = errors.New("不支持的话术生成模式")
	errScriptPolishUnavailable     = errors.New("「只润色」链路暂未实现。")
	errScriptAnalysisFailed        = errors.New("母版话术分析失败，请重试")
	errScriptAnalysisNoChain       = errors.New("母版未提取到可用的逻辑链，请重试")
	errScriptStructureUnavailable  = errors.New("当前话术生成结构尚未就绪")
)

type scriptTextMaterial struct {
	version            model.LiveAgentPlanVersion
	unit               volcengineScriptUnit
	facts              json.RawMessage
	redLines           string
	speechRequirements string
	persona            string
}

type scriptTextGenerationInput struct {
	material scriptTextMaterial
	// A preview loads the saved plan material only when its generator needs it.
	loadMaterial func() (scriptTextMaterial, error)
	session      int64
	progress     func(string)
	// Background supply shares one deadline between writing and TTS, starting after planning.
	writingContext func(context.Context) context.Context
}

// Generators produce text. Saving, synthesis and playback belong to the caller.
type scriptTextGenerator func(context.Context, scriptTextGenerationInput) (scriptRewriteOutput, error)

func (s *Server) generateScriptText(ctx context.Context, mode scriptGenerationMode, input scriptTextGenerationInput) (scriptRewriteOutput, error) {
	generators := map[scriptGenerationMode]scriptTextGenerator{
		scriptGenerationDefault:   s.generateRewriteScriptText,
		scriptGenerationRewrite:   s.generateRewriteScriptText,
		scriptGenerationPolish:    generatePolishScriptText,
		scriptGenerationSpecified: s.generateSpecifiedScriptText,
	}
	generate, ok := generators[mode]
	if !ok {
		return scriptRewriteOutput{}, errUnknownScriptGenerationMode
	}
	return generate(ctx, input)
}

func generatePolishScriptText(context.Context, scriptTextGenerationInput) (scriptRewriteOutput, error) {
	return scriptRewriteOutput{}, errScriptPolishUnavailable
}

func (s *Server) generateRewriteScriptText(ctx context.Context, input scriptTextGenerationInput) (scriptRewriteOutput, error) {
	material := input.material
	if input.loadMaterial != nil {
		var err error
		material, err = input.loadMaterial()
		if err != nil {
			return scriptRewriteOutput{}, err
		}
	}
	var requirement string
	if input.session > 0 {
		version := material.version
		requirement = s.scriptChainRequirement(ctx, version.TenantID, version.PlanID, input.session, material.unit.Text, material.unit.poolID())
		if strings.TrimSpace(requirement) == "" {
			return scriptRewriteOutput{}, errScriptStructureUnavailable
		}
		if doc, err := s.store.GetLiveAgentPlanPromptDocument(ctx, version.TenantID, version.PlanID); err == nil {
			material.speechRequirements = doc.Sections.SpeechRequirementsMD
			material.persona = doc.Sections.PersonaMD
		}
	} else {
		if input.progress != nil {
			input.progress("正在分析母版…")
		}
		analysis, err := s.analyzeScriptSample(ctx, material.unit.Text)
		if err != nil {
			return scriptRewriteOutput{}, fmt.Errorf("%w: %w", errScriptAnalysisFailed, err)
		}
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		target := scriptchain.TargetLength(rng)
		index := scriptchain.Pick(len(analysis.Chains), nil, rng)
		if index < 0 {
			return scriptRewriteOutput{}, errScriptAnalysisNoChain
		}
		requirement = scriptchain.RenderRequirement(analysis, analysis.Chains[index], target)
	}
	prompt := renderChainRewritePrompt(material.persona, requirement, material.speechRequirements, material.redLines, string(material.facts))
	if input.progress != nil {
		input.progress("正在仿写话术…")
	}
	if input.writingContext != nil {
		ctx = input.writingContext(ctx)
	}
	return s.writeScriptRewrite(ctx, material.version, material.unit, prompt)
}
