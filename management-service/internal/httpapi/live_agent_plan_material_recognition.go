package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
)

const maxLiveMaterialImageBytes int64 = 20 << 20

type liveMaterialImageRecognition struct {
	Text          string                    `json:"text"`
	VisualContext string                    `json:"visual_context"`
	Warnings      []string                  `json:"warnings"`
	Product       *liveMaterialImageProduct `json:"product,omitempty"`
}

type liveMaterialImageProduct struct {
	ProductName string `json:"product_name,omitempty"`
	Spec        string `json:"spec,omitempty"`
	DailyPrice  string `json:"daily_price,omitempty"`
	Quantity    string `json:"quantity,omitempty"`
	Audience    string `json:"audience,omitempty"`
}

func normalizeLiveMaterialImageRecognition(input liveMaterialImageRecognition) liveMaterialImageRecognition {
	input.Text = strings.TrimSpace(input.Text)
	input.VisualContext = strings.TrimSpace(input.VisualContext)
	if input.Warnings == nil {
		input.Warnings = []string{}
	}
	warnings := make([]string, 0, len(input.Warnings))
	seen := map[string]struct{}{}
	for _, item := range input.Warnings {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		warnings = append(warnings, item)
		if len(warnings) >= 8 {
			break
		}
	}
	input.Warnings = warnings
	if input.Product != nil {
		input.Product.ProductName = strings.TrimSpace(input.Product.ProductName)
		input.Product.Spec = strings.TrimSpace(input.Product.Spec)
		input.Product.DailyPrice = strings.TrimSpace(input.Product.DailyPrice)
		input.Product.Quantity = strings.TrimSpace(input.Product.Quantity)
		input.Product.Audience = strings.TrimSpace(input.Product.Audience)
		if input.Product.ProductName == "" &&
			input.Product.Spec == "" &&
			input.Product.DailyPrice == "" &&
			input.Product.Quantity == "" &&
			input.Product.Audience == "" {
			input.Product = nil
		}
	}
	return input
}

func recognizeLiveMaterialImage(ctx context.Context, mimeType string, raw []byte) (liveMaterialImageRecognition, string, string, int64, error) {
	mimeType = strings.TrimSpace(strings.ToLower(mimeType))
	switch mimeType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return liveMaterialImageRecognition{}, "", "", 0, errors.New("unsupported live material image type")
	}
	modelName := strings.TrimSpace(os.Getenv("LIVE_MATERIAL_VISION_MODEL"))
	if modelName == "" {
		modelName = "qwen3.8-flash"
	}
	dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	prompt := `你是直播素材图片识别器。只根据图片中直接可见的内容工作，不得补充图片没有显示的信息。

请完成三件事：
1. text：按自然阅读顺序逐字提取所有可辨认文字，尽量保留原有换行、数字、单位、标点和链接编号。看不清的地方写“[无法辨认]”，不要猜。
2. visual_context：只描述对直播素材有用、且图片中直接可见的非文字信息，例如商品外观、包装、表格/海报结构、明显的数量关系。不得推断品牌背景、功效、价格、产地、库存、快递、资质或活动规则。
3. product：如果图片明显是在展示一个商品，则只根据图片直接可见内容提取商品字段；没有显示的字段必须为空字符串。product_name 只写能从包装/标题直接确认的商品名称；spec 写规格或净含量；daily_price 仅在图片明确显示普通/日常/原价时填写；quantity 写明确显示的数量；audience 仅在图片明确写出适用人群时填写。图片不是商品资料时 product 返回 null。

严格返回 JSON：
{
  "text": "图片文字",
  "visual_context": "图片可见内容的客观描述",
  "warnings": ["模糊、遮挡、裁切等识别限制"],
  "product": {
    "product_name": "直接可见的商品名称",
    "spec": "直接可见的规格",
    "daily_price": "直接可见的日常/原价",
    "quantity": "直接可见的数量",
    "audience": "直接可见的适用人群"
  }
}`
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Model: modelName,
		Messages: []agentgateway.Message{{
			Role:      "user",
			Content:   prompt,
			ImageURLs: []string{dataURI},
		}},
		MaxTokens:      10000,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        90 * time.Second,
	})
	if err != nil {
		return liveMaterialImageRecognition{}, result.Provider, result.Model, result.LatencyMS, err
	}
	var output liveMaterialImageRecognition
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &output); err != nil {
		return liveMaterialImageRecognition{}, result.Provider, result.Model, result.LatencyMS, err
	}
	output = normalizeLiveMaterialImageRecognition(output)
	if output.Text == "" && output.VisualContext == "" {
		return liveMaterialImageRecognition{}, result.Provider, result.Model, result.LatencyMS, errors.New("empty image recognition result")
	}
	return output, result.Provider, result.Model, result.LatencyMS, nil
}

func (s *Server) liveAgentPlanScriptRecognizeImagePreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLiveMaterialImageBytes+2*1024*1024)
	if err := r.ParseMultipartForm(maxLiveMaterialImageBytes); err != nil {
		writeError(w, http.StatusBadRequest, "图片过大或上传格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择要识别的图片")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxLiveMaterialImageBytes {
		writeError(w, http.StatusBadRequest, "图片大小不合法，单张最大20MB")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxLiveMaterialImageBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取图片失败")
		return
	}
	if int64(len(raw)) > maxLiveMaterialImageBytes {
		writeError(w, http.StatusBadRequest, "图片大小超过20MB")
		return
	}
	mimeType := strings.ToLower(strings.TrimSpace(header.Header.Get("Content-Type")))
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(raw)
	}
	switch mimeType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		writeError(w, http.StatusBadRequest, "直播素材图片仅支持 JPG、PNG、WEBP")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	output, provider, modelName, latencyMS, err := recognizeLiveMaterialImage(ctx, mimeType, raw)
	if err != nil {
		writeError(w, http.StatusBadGateway, "图片识别暂时失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"text":           output.Text,
		"visual_context": output.VisualContext,
		"warnings":       output.Warnings,
		"product":        output.Product,
		"provider":       provider,
		"model":          modelName,
		"latency_ms":     latencyMS,
		"persisted":      false,
	})
}
